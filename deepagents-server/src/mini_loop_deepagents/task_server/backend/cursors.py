"""Version-one signed event cursors, scoped to an owner and retained Run."""

from __future__ import annotations

import base64
import hmac
import json
from typing import Any
from uuid import UUID

from ..protocol import TaskInvalidRequest, canonical_json

def _encoded(value: bytes) -> str:
    return base64.urlsafe_b64encode(value).decode("ascii").rstrip("=")


def _decoded(value: str) -> bytes:
    return base64.b64decode(value + "=" * (-len(value) % 4), altchars=b"-_", validate=True)


class CursorCodec:
    def __init__(self, secret: bytes | str):
        secret = secret.encode() if isinstance(secret, str) else secret
        if not isinstance(secret, bytes) or len(secret) < 32:
            raise ValueError("A fixed deployment cursor secret of at least 32 bytes is required")
        self._secret = secret

    def encode(self, namespace: str, owner: str, task_id: str, run_id: str, before: int) -> str:
        value = {"v": 1, "namespace": namespace, "owner": owner,
                 "task_id": task_id, "run_id": run_id, "before_event_id": before}
        body = _encoded(canonical_json(value))
        signature = _encoded(hmac.digest(self._secret, body.encode("ascii"), "sha256"))
        return body + "." + signature

    def decode(self, namespace: str, cursor: str, owner: str, task_id: str) -> dict[str, Any]:
        try:
            if not isinstance(cursor, str) or len(cursor) > 4096:
                raise ValueError
            body, signature = cursor.split(".")
            if not hmac.compare_digest(_decoded(signature), hmac.digest(self._secret, body.encode("ascii"), "sha256")):
                raise ValueError
            value = json.loads(_decoded(body))
            if (set(value) != {"v", "namespace", "owner", "task_id", "run_id", "before_event_id"}
                    or type(value["v"]) is not int or value["v"] != 1 or value["owner"] != owner
                    or value["namespace"] != namespace or value["task_id"] != task_id
                    or type(value["before_event_id"]) is not int or value["before_event_id"] <= 0
                    or str(UUID(value["run_id"])) != value["run_id"]):
                raise ValueError
            return value
        except (ValueError, TypeError, KeyError, UnicodeError, AttributeError):
            raise TaskInvalidRequest("cursor") from None
