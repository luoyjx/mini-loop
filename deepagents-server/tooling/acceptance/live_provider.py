"""Fail-closed P1.4 live-provider acceptance against an existing Task deployment.

The committed P0 profile never grants spend. ``run`` additionally requires a
short-lived private approval file, an explicit environment acknowledgement and
file-backed Task/LangSmith credentials. The public receipt contains hashes and
measurements, while the synthetic result snapshot stays in the private output
directory. Missing provider usage or cost is inconclusive, never zero.
"""

from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation, ROUND_CEILING
import hashlib
import json
import math
import os
from pathlib import Path
import re
import ssl
import stat
import subprocess
import time
from typing import Any, Iterable, Mapping, Sequence
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode, urlsplit
from urllib.request import build_opener, HTTPSHandler, HTTPRedirectHandler, ProxyHandler, Request
from uuid import UUID, uuid4

import langsmith
import requests

from tooling.paths import MODULE
from tooling.acceptance.production_profile import load_profile

DEFAULT_CASES = MODULE / "docs" / "PRODUCTION_PROVIDER_CASES.json"
DEFAULT_PROFILE = MODULE / "docs" / "PRODUCTION_ACCEPTANCE_PROFILE.json"
CASE_KIND = "mini-loop-task-live-provider-cases"
APPROVAL_KIND = "mini-loop-task-live-provider-approval"
ACKNOWLEDGEMENT = "approved-live-provider-run"
MAX_JSON_BYTES = 2 * 1024 * 1024
MAX_TRACE_RUNS = 1000
TRACE_TOOL_NAMES = frozenset({"task", "write_file", "read_file", "write_todos"})
TRACE_TOPOLOGY_NAMES = frozenset({
    "RunWorkflow:TaskWorkflowV2", "RunActivity:deepagents.invoke_model", "ChatOpenAI",
})
SAFE_LABEL = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:/@+-]{0,255}\Z")
CASE_ID = re.compile(r"[a-z0-9][a-z0-9-]{0,95}\Z")
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
COMMIT = re.compile(r"[0-9a-f]{40}\Z")
TASK_ID = re.compile(r"task-[0-9a-f]{64}\Z")


class LiveProviderError(ValueError):
    """Stable error code that never includes credentials, prompts or remote bodies."""


class Inconclusive(LiveProviderError):
    """The run produced evidence, but a mandatory measurement stayed unknown."""


def _reject_constant(_value: str) -> None:
    raise LiveProviderError("json_nonfinite")


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            raise LiveProviderError("json_duplicate_key")
        value[key] = item
    return value


def _strict_json_bytes(raw: bytes) -> Any:
    if len(raw) > MAX_JSON_BYTES:
        raise LiveProviderError("json_too_large")
    try:
        return json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_object,
                          parse_constant=_reject_constant)
    except (UnicodeError, json.JSONDecodeError):
        raise LiveProviderError("json_invalid") from None


def _read_json(path: Path, *, maximum: int = MAX_JSON_BYTES, private: bool = False) -> Any:
    try:
        details = path.lstat()
        if (not stat.S_ISREG(details.st_mode) or details.st_size > maximum
                or private and details.st_uid != os.geteuid()):
            raise LiveProviderError("evidence_file_invalid")
        if private and (details.st_mode & 0o077):
            raise LiveProviderError("private_file_permissions")
        raw = path.read_bytes()
    except LiveProviderError:
        raise
    except OSError:
        raise LiveProviderError("evidence_file_unavailable") from None
    if len(raw) > maximum:
        raise LiveProviderError("evidence_file_invalid")
    return _strict_json_bytes(raw)


def _read_secret(path: Path) -> str:
    try:
        details = path.lstat()
        if (not stat.S_ISREG(details.st_mode) or details.st_uid != os.geteuid()
                or details.st_size > 16 * 1024 or details.st_mode & 0o077):
            raise LiveProviderError("secret_file_invalid")
        raw = path.read_bytes()
    except LiveProviderError:
        raise
    except OSError:
        raise LiveProviderError("secret_file_unavailable") from None
    if raw.endswith(b"\r\n"):
        raw = raw[:-2]
    elif raw.endswith(b"\n"):
        raw = raw[:-1]
    if not raw or b"\n" in raw or b"\r" in raw or any(byte < 33 or byte > 126 for byte in raw):
        raise LiveProviderError("secret_file_invalid")
    return raw.decode("ascii")


def _object(value: Any, keys: set[str], code: str) -> dict[str, Any]:
    if type(value) is not dict or set(value) != keys:
        raise LiveProviderError(code)
    return value


def _text(value: Any, code: str, *, pattern: re.Pattern[str] = SAFE_LABEL) -> str:
    if type(value) is not str or not pattern.fullmatch(value):
        raise LiveProviderError(code)
    return value


def _integer(value: Any, code: str, minimum: int = 0, maximum: int | None = None) -> int:
    if type(value) is not int or value < minimum or (maximum is not None and value > maximum):
        raise LiveProviderError(code)
    return value


def _canonical(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"),
                      allow_nan=False).encode("utf-8")


def _sha256(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def _canonical_uuid(value: Any, code: str) -> str:
    try:
        parsed = value if isinstance(value, UUID) else UUID(value) if isinstance(value, str) else None
        if parsed is None or (isinstance(value, str) and str(parsed) != value):
            raise ValueError()
    except (TypeError, ValueError, AttributeError):
        raise LiveProviderError(code) from None
    return str(parsed)


def load_cases(path: Path = DEFAULT_CASES) -> dict[str, dict[str, Any]]:
    value = _read_json(path)
    root = _object(value, {"schema_version", "kind", "status", "cases"}, "cases_shape")
    if root["schema_version"] != 1 or root["kind"] != CASE_KIND or root["status"] != "fixed-p1.4-cases":
        raise LiveProviderError("cases_identity")
    if type(root["cases"]) is not list or len(root["cases"]) != 1:
        raise LiveProviderError("cases_count")
    result: dict[str, dict[str, Any]] = {}
    for raw_case in root["cases"]:
        case = _object(raw_case, {"id", "model", "message", "expected"}, "case_shape")
        case_id = _text(case["id"], "case_id", pattern=CASE_ID)
        if case_id in result or case["model"] != "openai:gpt-4.1-mini":
            raise LiveProviderError("case_identity")
        if type(case["message"]) is not str or not case["message"].strip() or len(case["message"].encode()) > 16 * 1024:
            raise LiveProviderError("case_message")
        expected = _object(case["expected"], {
            "minimum_can_count", "minimum_model_boundaries", "require_summary",
            "required_completed_todos", "expected_trace_tool_runs", "required_files",
        }, "case_expected_shape")
        _integer(expected["minimum_can_count"], "case_can_count", 1, 63)
        _integer(expected["minimum_model_boundaries"], "case_model_boundaries", 9, 511)
        if expected["require_summary"] is not True:
            raise LiveProviderError("case_summary")
        todos = expected["required_completed_todos"]
        if type(todos) is not list or not todos or len(set(todos)) != len(todos):
            raise LiveProviderError("case_todos")
        for item in todos:
            if type(item) is not str or not item or len(item) > 256:
                raise LiveProviderError("case_todos")
        tools = expected["expected_trace_tool_runs"]
        if type(tools) is not dict or set(tools) != {"task", "write_file", "read_file", "write_todos"}:
            raise LiveProviderError("case_tools")
        for count in tools.values():
            _integer(count, "case_tools", 1, 100)
        files = expected["required_files"]
        if type(files) is not dict or len(files) < 3:
            raise LiveProviderError("case_files")
        for file_path, content in files.items():
            if (type(file_path) is not str or not file_path.startswith("/acceptance/")
                    or ".." in file_path or type(content) is not str or len(content.encode()) > 1024):
                raise LiveProviderError("case_files")
        result[case_id] = case
    return result


def _parse_time(value: Any, code: str) -> datetime:
    if type(value) is not str or not value.endswith("Z"):
        raise LiveProviderError(code)
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError:
        raise LiveProviderError(code) from None
    return parsed


def _git_state() -> tuple[str, bool]:
    try:
        commit = subprocess.run(["git", "rev-parse", "HEAD"], cwd=MODULE, check=True,
                                capture_output=True, text=True, timeout=5).stdout.strip()
        dirty = bool(subprocess.run(
            ["git", "status", "--porcelain"], cwd=MODULE,
            check=True, capture_output=True, text=True, timeout=5,
        ).stdout)
    except (OSError, subprocess.SubprocessError):
        raise LiveProviderError("git_state_unavailable") from None
    if not COMMIT.fullmatch(commit):
        raise LiveProviderError("git_state_invalid")
    return commit, dirty


def load_approval(path: Path, cases: Mapping[str, dict[str, Any]], profile: dict[str, Any],
                  now: datetime | None = None) -> dict[str, Any]:
    approval = _object(_read_json(path, maximum=64 * 1024, private=True), {
        "schema_version", "kind", "approved", "approved_by", "approved_at", "expires_at",
        "source_commit", "image_digest", "case_ids", "model", "environment", "budget",
    }, "approval_shape")
    if approval["schema_version"] != 1 or approval["kind"] != APPROVAL_KIND or approval["approved"] is not True:
        raise LiveProviderError("operator_approval_required")
    _text(approval["approved_by"], "approval_identity")
    approved_at = _parse_time(approval["approved_at"], "approval_time")
    expires_at = _parse_time(approval["expires_at"], "approval_time")
    current = now or datetime.now(timezone.utc)
    if not approved_at <= current < expires_at or (expires_at - approved_at).total_seconds() > 24 * 3600:
        raise LiveProviderError("approval_expired")
    if not COMMIT.fullmatch(approval["source_commit"]) or not DIGEST.fullmatch(approval["image_digest"]):
        raise LiveProviderError("approval_artifact_identity")
    commit, dirty = _git_state()
    if dirty or approval["source_commit"] != commit:
        raise LiveProviderError("approval_source_mismatch")
    _text(approval["model"], "approval_model")
    if approval["model"] not in profile["provider_budget"]["allowed_models"]:
        raise LiveProviderError("approval_model")
    case_ids = approval["case_ids"]
    if type(case_ids) is not list or case_ids != list(cases):
        raise LiveProviderError("approval_cases")
    environment = _object(approval["environment"], {
        "provider", "region", "temporal_namespace", "data_residency", "license",
    }, "approval_environment")
    for item in environment.values():
        _text(item, "approval_environment")
    if environment["provider"] != approval["model"].split(":", 1)[0]:
        raise LiveProviderError("approval_environment")
    budget = _object(approval["budget"], {
        "max_provider_calls", "max_total_tokens", "max_cost_cents",
        "calendar_month_spend_before_cents",
    }, "approval_budget")
    limits = profile["provider_budget"]
    for key, maximum in (
        ("max_provider_calls", limits["max_provider_calls_per_live_run"]),
        ("max_total_tokens", limits["max_total_tokens_per_live_run"]),
        ("max_cost_cents", limits["max_cost_cents_per_live_run"]),
        ("calendar_month_spend_before_cents", limits["max_cost_cents_per_calendar_month"]),
    ):
        _integer(budget[key], "approval_budget", 0 if key.startswith("calendar") else 1, maximum)
    if budget["calendar_month_spend_before_cents"] + budget["max_cost_cents"] > limits["max_cost_cents_per_calendar_month"]:
        raise LiveProviderError("approval_monthly_budget")
    return approval


def _validate_url(value: str, *, langsmith_endpoint: bool = False) -> str:
    try:
        parsed = urlsplit(value)
        valid = (parsed.scheme == "https" and parsed.hostname is not None
                 and parsed.username is None and parsed.password is None
                 and parsed.path in {"", "/"} and not parsed.query and not parsed.fragment
                 and parsed.port in ({None, 443} if langsmith_endpoint else {None, 443, 8443}))
    except (TypeError, ValueError):
        valid = False
    if not valid or len(value) > 512 or not value.isascii():
        raise LiveProviderError("endpoint_invalid")
    return value.rstrip("/")


class TaskAPI:
    def __init__(self, base_url: str, token: str, *, timeout: float = 20.0):
        self.base_url = _validate_url(base_url)
        self._token = token
        self._timeout = timeout
        self._opener = build_opener(
            ProxyHandler({}), _NoRedirect(), HTTPSHandler(context=ssl.create_default_context()),
        )

    def request(self, method: str, path: str, *, body: Any = None,
                idempotency_key: str | None = None) -> tuple[int, dict[str, Any]]:
        headers = {"Authorization": "Bearer " + self._token, "Accept": "application/json"}
        data = None
        if body is not None:
            data = _canonical(body)
            headers["Content-Type"] = "application/json"
        if idempotency_key is not None:
            headers["Idempotency-Key"] = idempotency_key
        request = Request(self.base_url + path, data=data, headers=headers, method=method)
        try:
            with self._opener.open(request, timeout=self._timeout) as response:
                raw = response.read(MAX_JSON_BYTES + 1)
                return response.status, _strict_json_bytes(raw)
        except HTTPError as error:
            try:
                error.read(MAX_JSON_BYTES + 1)
            except Exception:
                pass
            raise LiveProviderError("task_api_http_error") from None
        except (URLError, TimeoutError, OSError):
            raise LiveProviderError("task_api_unavailable") from None

    def cancel(self, task_id: str) -> None:
        try:
            self.request("POST", f"/v1/tasks/{quote(task_id, safe='')}/cancel")
        except LiveProviderError:
            pass


class _NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def _run_value(run: Any, key: str, default: Any = None) -> Any:
    return run.get(key, default) if isinstance(run, Mapping) else getattr(run, key, default)


def _metadata(run: Any) -> dict[str, Any]:
    extra = _run_value(run, "extra")
    value = extra.get("metadata") if isinstance(extra, Mapping) else None
    return dict(value) if isinstance(value, Mapping) else {}


def _number(value: Any, *, integer: bool = False) -> int | Decimal | None:
    if isinstance(value, bool) or value is None:
        return None
    try:
        result = Decimal(str(value))
    except (InvalidOperation, ValueError, TypeError):
        return None
    if not result.is_finite() or result < 0 or (integer and result != result.to_integral_value()):
        return None
    return int(result) if integer else result


def summarize_traces(runs: Iterable[Any], task_id: str, model: str) -> dict[str, Any]:
    provider, model_name = model.split(":", 1)
    selected = []
    for run in runs:
        metadata = _metadata(run)
        identities = {metadata.get("task_id"), metadata.get("thread_id"),
                      metadata.get("temporalWorkflowID")}
        if task_id in identities:
            selected.append(run)
    for run in selected:
        for key in ("id", "parent_run_id"):
            raw = _run_value(run, key)
            if raw is None and key == "parent_run_id":
                continue
            _canonical_uuid(raw, "trace_identity_invalid")
    llm_runs = []
    for run in selected:
        metadata = _metadata(run)
        if (_run_value(run, "run_type") == "llm" and metadata.get("ls_provider") == provider
                and metadata.get("ls_model_name") == model_name and _run_value(run, "end_time") is not None):
            llm_runs.append(run)
    total_tokens = 0
    total_cost = Decimal("0")
    unknown = []
    failed_provider_calls = 0
    for run in llm_runs:
        metadata = _metadata(run)
        usage = metadata.get("usage_metadata") if isinstance(metadata.get("usage_metadata"), Mapping) else {}
        tokens = _number(_run_value(run, "total_tokens"), integer=True)
        if tokens is None:
            tokens = _number(usage.get("total_tokens"), integer=True)
        cost = _number(_run_value(run, "total_cost"))
        if cost is None:
            cost = _number(usage.get("total_cost"))
        if tokens is None or cost is None:
            unknown.append(str(_run_value(run, "id", "unknown")))
        else:
            total_tokens += tokens
            total_cost += cost
        if _run_value(run, "error") is not None:
            failed_provider_calls += 1
    unsafe = []
    for run in selected:
        extra = _run_value(run, "extra") or {}
        if (_run_value(run, "inputs") not in (None, {}) or _run_value(run, "outputs") not in (None, {})
                or _run_value(run, "serialized") not in (None, {})
                or _run_value(run, "attachments") not in (None, {})
                or _run_value(run, "events") not in (None, [])
                or (isinstance(extra, Mapping) and any(key in extra for key in ("runtime", "invocation_params", "options")))):
            unsafe.append(str(_run_value(run, "id", "unknown")))
    names = Counter(
        str(_run_value(run, "name")) for run in selected
        if _run_value(run, "name") in TRACE_TOPOLOGY_NAMES
    )
    completed_tools = Counter(
        str(_run_value(run, "name", "unknown")) for run in selected
        if _run_value(run, "run_type") == "tool" and _run_value(run, "end_time") is not None
        and _run_value(run, "error") is None and _run_value(run, "name") in TRACE_TOOL_NAMES
    )
    unexpected_tool_run_count = sum(
        1 for run in selected if _run_value(run, "run_type") == "tool"
        and _run_value(run, "end_time") is not None and _run_value(run, "error") is None
        and _run_value(run, "name") not in TRACE_TOOL_NAMES
    )
    failed_run_ids = sorted(
        str(_run_value(run, "id", "unknown")) for run in selected
        if _run_value(run, "error") is not None
    )
    parent_ids = {str(_run_value(run, "parent_run_id")) for run in selected if _run_value(run, "parent_run_id")}
    return {
        "run_count": len(selected), "provider_call_count": len(llm_runs),
        "total_tokens": total_tokens,
        "total_cost_cents": int((total_cost * 100).to_integral_value(rounding=ROUND_CEILING)),
        "unknown_provider_run_ids": sorted(unknown),
        "failed_provider_calls": failed_provider_calls, "failed_run_ids": failed_run_ids,
        "unsafe_run_ids": sorted(unsafe), "parent_link_count": len(parent_ids),
        "run_names": dict(sorted(names.items())),
        "completed_tool_runs": dict(sorted(completed_tools.items())),
        "unexpected_tool_run_count": unexpected_tool_run_count,
    }


class TraceReader:
    def __init__(self, endpoint: str, api_key: str, project: str):
        self.project = _text(project, "trace_project")
        session = _DirectSession()
        self.client = langsmith.Client(api_url=_validate_url(endpoint, langsmith_endpoint=True),
                                       api_key=api_key, timeout_ms=20_000, session=session)

    def read(self, task_id: str, started_at: datetime, model: str) -> dict[str, Any]:
        try:
            runs = list(self.client.list_runs(project_name=self.project, start_time=started_at,
                                              limit=MAX_TRACE_RUNS))
        except Exception:
            raise LiveProviderError("trace_query_unavailable") from None
        if len(runs) >= MAX_TRACE_RUNS:
            raise Inconclusive("trace_query_limit_reached")
        return summarize_traces(runs, task_id, model)


class _DirectSession(requests.Session):
    """Do not send the read key through ambient proxies or redirects."""

    def __init__(self) -> None:
        super().__init__()
        self.trust_env = False

    def request(self, method, url, **kwargs):
        kwargs["allow_redirects"] = False
        return super().request(method, url, **kwargs)


def validate_result(value: Any, case: dict[str, Any], task_id: str,
                    first_run_id: str) -> dict[str, Any]:
    if not isinstance(value, Mapping) or value.get("status") != "succeeded" or value.get("ready") is not True:
        raise LiveProviderError("task_not_succeeded")
    snapshot = value.get("result")
    if not isinstance(snapshot, Mapping):
        raise LiveProviderError("result_missing")
    final_run_id = _canonical_uuid(value.get("run_id"), "result_identity")
    identity = snapshot.get("identity")
    if (snapshot.get("schema_version") != 2 or snapshot.get("version") != "task-v2"
            or snapshot.get("status") != "completed" or snapshot.get("next") != []
            or not isinstance(identity, Mapping) or identity.get("task_id") != task_id
            or identity.get("workflow_id") != task_id
            or identity.get("first_execution_run_id") != first_run_id):
        raise LiveProviderError("result_identity")
    expected = case["expected"]
    can_count = _integer(snapshot.get("can_count"), "result_can_count", 0, 63)
    boundaries = _integer(snapshot.get("total_model_boundaries"), "result_boundaries", 0, 511)
    if can_count < expected["minimum_can_count"] or boundaries < expected["minimum_model_boundaries"]:
        raise LiveProviderError("result_cross_segment_missing")
    files = snapshot.get("files")
    if not isinstance(files, Mapping):
        raise LiveProviderError("result_files")
    for path, content in expected["required_files"].items():
        record = files.get(path)
        if (not isinstance(record, Mapping) or record.get("content") != content
                or not record.get("created_at") or not record.get("modified_at")):
            raise LiveProviderError("result_files")
    todos = snapshot.get("todos")
    completed = {item.get("content") for item in todos if isinstance(item, Mapping)
                 and item.get("status") == "completed"} if isinstance(todos, list) else set()
    if completed != set(expected["required_completed_todos"]) or len(todos) != len(completed):
        raise LiveProviderError("result_todos")
    state = snapshot.get("state")
    event = state.get("_summarization_event") if isinstance(state, Mapping) else None
    if (not isinstance(event, Mapping) or not event.get("summary_message")
            or not isinstance(state.get("_summarization_session_id"), str)
            or not state.get("_summarization_session_id")
            or event.get("file_path") not in files):
        raise LiveProviderError("result_summary")
    if set(files) != set(expected["required_files"]) | {event["file_path"]}:
        raise LiveProviderError("result_files")
    return {
        "status": "passed", "can_count": can_count, "total_model_boundaries": boundaries,
        "required_file_count": len(expected["required_files"]),
        "completed_todo_count": len(completed),
        "snapshot_sha256": _sha256(_canonical(snapshot)), "final_run_id": final_run_id,
    }


def read_history(api: TaskAPI, task_id: str, final_run_id: str,
                 first_run_id: str, expected_runs: int) -> dict[str, Any]:
    run_id: str | None = final_run_id
    rows = []
    event_count = 0
    while run_id is not None and len(rows) <= 64:
        cursor = None
        event_types = []
        navigation = None
        for _ in range(64):
            query = {"limit": "100", "run_id": run_id}
            if cursor is not None:
                query["cursor"] = cursor
            status, page = api.request(
                "GET", f"/v1/tasks/{quote(task_id, safe='')}/events?{urlencode(query)}",
            )
            if status != 200 or not isinstance(page, Mapping) or page.get("run_id") != run_id:
                raise LiveProviderError("history_read")
            navigation = page
            events = page.get("events")
            if not isinstance(events, list):
                raise LiveProviderError("history_read")
            for event in events:
                if not isinstance(event, Mapping) or not isinstance(event.get("type"), str):
                    raise LiveProviderError("history_read")
                event_types.append(event["type"])
            cursor = page.get("next_cursor")
            if cursor is None:
                break
            if not isinstance(cursor, str):
                raise LiveProviderError("history_read")
        else:
            raise LiveProviderError("history_page_budget")
        if navigation is None or navigation.get("history_navigation_incomplete") is not False:
            raise LiveProviderError("history_incomplete")
        previous_run_id = navigation.get("previous_run_id")
        next_run_id = navigation.get("next_run_id")
        if previous_run_id is not None:
            previous_run_id = _canonical_uuid(previous_run_id, "history_chain")
        if next_run_id is not None:
            next_run_id = _canonical_uuid(next_run_id, "history_chain")
        rows.append({"run_id": run_id, "previous_run_id": previous_run_id,
                     "next_run_id": next_run_id, "event_types": event_types})
        event_count += len(event_types)
        run_id = navigation.get("previous_run_id")
    if len(rows) != expected_runs or rows[-1]["run_id"] != first_run_id:
        raise LiveProviderError("history_chain")
    chronological = list(reversed(rows))
    for index, row in enumerate(chronological):
        previous = chronological[index - 1]["run_id"] if index else None
        following = chronological[index + 1]["run_id"] if index + 1 < len(chronological) else None
        if row["previous_run_id"] != previous or row["next_run_id"] != following:
            raise LiveProviderError("history_chain")
        if "workflow_task_failed" in row["event_types"]:
            raise LiveProviderError("history_workflow_task_failed")
    return {"run_count": len(rows), "event_count": event_count,
            "run_ids": [row["run_id"] for row in chronological], "workflow_task_failed": 0}


def _write_json(path: Path, value: Any) -> str:
    try:
        raw = json.dumps(value, sort_keys=True, ensure_ascii=False, indent=2,
                         allow_nan=False).encode("utf-8") + b"\n"
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
    except (OSError, TypeError, ValueError):
        try:
            path.unlink()
        except OSError:
            pass
        raise LiveProviderError("evidence_write_failed") from None
    return _sha256(raw)


def _budget_guard(summary: dict[str, Any], approval: dict[str, Any], profile: dict[str, Any]) -> None:
    budget = approval["budget"]
    if summary["provider_call_count"] > budget["max_provider_calls"]:
        raise LiveProviderError("provider_call_budget_exceeded")
    if summary["total_tokens"] > budget["max_total_tokens"]:
        raise LiveProviderError("provider_token_budget_exceeded")
    if summary["total_cost_cents"] > budget["max_cost_cents"]:
        raise LiveProviderError("provider_cost_budget_exceeded")
    if (budget["calendar_month_spend_before_cents"] + summary["total_cost_cents"]
            > profile["provider_budget"]["max_cost_cents_per_calendar_month"]):
        raise LiveProviderError("provider_monthly_budget_exceeded")


def run_live(args: argparse.Namespace) -> dict[str, Any]:
    try:
        committed_contracts = (args.profile.resolve() == DEFAULT_PROFILE.resolve()
                               and args.cases.resolve() == DEFAULT_CASES.resolve())
    except (OSError, RuntimeError):
        committed_contracts = False
    if not committed_contracts:
        raise LiveProviderError("committed_contract_required")
    profile = load_profile(args.profile)
    cases = load_cases(args.cases)
    case = cases.get(args.case_id)
    if case is None:
        raise LiveProviderError("case_unknown")
    if os.environ.get("MINILOOP_TASK_ACCEPTANCE_REAL") != ACKNOWLEDGEMENT:
        raise LiveProviderError("execution_acknowledgement_required")
    approval = load_approval(args.approval_file, cases, profile)
    if approval["model"] != case["model"]:
        raise LiveProviderError("approval_model")
    task_token = _read_secret(args.task_token_file)
    trace_key = _read_secret(args.langsmith_api_key_file)
    api = TaskAPI(args.api_base_url, task_token)
    traces = TraceReader(args.langsmith_endpoint, trace_key, args.langsmith_project)
    output = args.output_dir
    try:
        output.mkdir(mode=0o700, parents=False, exist_ok=False)
        details = output.lstat()
        if (not stat.S_ISDIR(details.st_mode) or details.st_uid != os.geteuid()
                or stat.S_IMODE(details.st_mode) != 0o700):
            raise OSError()
    except OSError:
        raise LiveProviderError("output_directory_invalid") from None
    started_at = datetime.now(timezone.utc)
    started_monotonic = time.monotonic()
    key = f"p1-4-{case['id']}-{started_at.strftime('%Y%m%d%H%M%S')}-{uuid4().hex[:8]}"
    receipt: dict[str, Any] = {
        "schema_version": 1, "kind": "mini-loop-task-live-provider-receipt",
        "status": "running", "case_id": case["id"], "model": case["model"],
        "case_manifest_sha256": _sha256(args.cases.read_bytes()),
        "message_sha256": _sha256(case["message"].encode()),
        "approval_sha256": _sha256(_canonical(approval)),
        "approver_sha256": _sha256(approval["approved_by"].encode()),
        "source_commit": approval["source_commit"], "image_digest": approval["image_digest"],
        "environment": approval["environment"], "budget": approval["budget"],
        "started_at": started_at.isoformat().replace("+00:00", "Z"),
    }
    task_id = None
    try:
        submit_started = time.monotonic()
        status, submitted = api.request("POST", "/v1/tasks", body={"message": case["message"]},
                                        idempotency_key=key)
        if (status != 202 or not isinstance(submitted, Mapping)
                or not TASK_ID.fullmatch(str(submitted.get("task_id", "")))
                or submitted.get("write_confirmed") is not True or submitted.get("reused") is not False):
            raise LiveProviderError("task_submission")
        task_id = submitted["task_id"]
        first_run_id = _canonical_uuid(submitted.get("first_execution_run_id"), "task_submission")
        receipt.update(task_id=task_id, first_execution_run_id=first_run_id,
                       submit_latency_ms=round((time.monotonic() - submit_started) * 1000, 3))
        deadline = started_monotonic + args.deadline_seconds
        result = None
        last_trace = None
        while time.monotonic() < deadline:
            state_status, state = api.request("GET", f"/v1/tasks/{quote(task_id, safe='')}")
            if state_status != 200 or not isinstance(state, Mapping):
                raise LiveProviderError("task_status_read")
            last_trace = traces.read(task_id, started_at, case["model"])
            _budget_guard(last_trace, approval, profile)
            if last_trace["unknown_provider_run_ids"]:
                raise Inconclusive("provider_usage_unknown")
            if last_trace["unsafe_run_ids"]:
                raise LiveProviderError("trace_content_leak")
            if last_trace["failed_run_ids"]:
                raise LiveProviderError("trace_run_failed")
            if state.get("status") in {"succeeded", "failed", "cancelled", "timed_out", "terminated"}:
                result_status, result = api.request("GET", f"/v1/tasks/{quote(task_id, safe='')}/result")
                if result_status != 200 or not isinstance(result, Mapping):
                    raise LiveProviderError("task_result_read")
                break
            time.sleep(2)
        if result is None:
            raise LiveProviderError("task_deadline_exceeded")
        private_sha = _write_json(output / "private-result.json", result)
        validation = validate_result(result, case, task_id, first_run_id)
        history = read_history(api, task_id, str(validation["final_run_id"]), first_run_id,
                               validation["can_count"] + 1)
        trace_deadline = time.monotonic() + 60
        trace_complete = False
        while time.monotonic() < trace_deadline:
            last_trace = traces.read(task_id, started_at, case["model"])
            _budget_guard(last_trace, approval, profile)
            if last_trace["unknown_provider_run_ids"]:
                raise Inconclusive("provider_usage_unknown")
            if last_trace["unsafe_run_ids"]:
                raise LiveProviderError("trace_content_leak")
            if last_trace["failed_run_ids"]:
                raise LiveProviderError("trace_run_failed")
            tool_runs = last_trace["completed_tool_runs"]
            expected_tools = case["expected"]["expected_trace_tool_runs"]
            if (last_trace["unexpected_tool_run_count"]
                    or any(tool_runs.get(name, 0) > count for name, count in expected_tools.items())):
                raise LiveProviderError("trace_tool_coverage")
            names = last_trace["run_names"]
            if (last_trace["provider_call_count"] >= validation["total_model_boundaries"]
                    and TRACE_TOPOLOGY_NAMES <= set(names)
                    and last_trace["parent_link_count"] >= 3 and tool_runs == expected_tools):
                trace_complete = True
                break
            time.sleep(2)
        if last_trace is None or not last_trace["provider_call_count"]:
            raise Inconclusive("trace_provider_calls_missing")
        if (not trace_complete and (not TRACE_TOPOLOGY_NAMES <= set(last_trace["run_names"])
                                   or last_trace["parent_link_count"] < 3
                                   or last_trace["provider_call_count"] < validation["total_model_boundaries"])):
            raise LiveProviderError("trace_topology_incomplete")
        if not trace_complete:
            raise LiveProviderError("trace_tool_coverage")
        completed_at = datetime.now(timezone.utc)
        receipt.update({
            "status": "passed", "completed_at": completed_at.isoformat().replace("+00:00", "Z"),
            "elapsed_seconds": round(time.monotonic() - started_monotonic, 3),
            "result_validation": validation, "history": history, "trace": last_trace,
            "private_result": {"path": "private-result.json", "sha256": private_sha},
        })
        rendered = _canonical(receipt)
        if task_token.encode() in rendered or trace_key.encode() in rendered:
            raise LiveProviderError("credential_in_receipt")
        _write_json(output / "receipt.json", receipt)
        return receipt
    except (LiveProviderError, Inconclusive) as error:
        if task_id is not None:
            api.cancel(task_id)
        failure = {**receipt, "status": "inconclusive" if isinstance(error, Inconclusive) else "failed",
                   "failure_code": str(error),
                   "completed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")}
        try:
            _write_json(output / "receipt.json", failure)
        except LiveProviderError:
            pass
        raise


def check_summary(profile_path: Path, cases_path: Path) -> dict[str, Any]:
    profile = load_profile(profile_path)
    cases = load_cases(cases_path)
    return {
        "status": "ready", "live_execution": "not-authorized-by-committed-profile",
        "provider_execution_authorized": profile["provider_budget"]["execution_authorized"],
        "operator_approval_required": profile["provider_budget"]["operator_approval_required"],
        "case_ids": list(cases), "case_manifest_sha256": _sha256(cases_path.read_bytes()),
        "allowed_models": profile["provider_budget"]["allowed_models"],
        "unknown_usage_action": profile["provider_budget"]["unknown_usage_action"],
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    check = subparsers.add_parser("check", help="validate committed cases without live execution")
    check.add_argument("--profile", type=Path, default=DEFAULT_PROFILE)
    check.add_argument("--cases", type=Path, default=DEFAULT_CASES)
    run = subparsers.add_parser("run", help="execute one explicitly approved live case")
    run.add_argument("--profile", type=Path, default=DEFAULT_PROFILE)
    run.add_argument("--cases", type=Path, default=DEFAULT_CASES)
    run.add_argument("--case-id", default="planning-files-subagents-summary-can-v1")
    run.add_argument("--approval-file", type=Path, required=True)
    run.add_argument("--api-base-url", required=True)
    run.add_argument("--task-token-file", type=Path, required=True)
    run.add_argument("--langsmith-endpoint", default="https://api.smith.langchain.com")
    run.add_argument("--langsmith-project", required=True)
    run.add_argument("--langsmith-api-key-file", type=Path, required=True)
    run.add_argument("--output-dir", type=Path, required=True)
    run.add_argument("--deadline-seconds", type=int, default=900, choices=range(60, 7201))
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        value = (check_summary(args.profile, args.cases) if args.command == "check"
                 else run_live(args))
    except LiveProviderError as error:
        print(json.dumps({"status": "blocked" if str(error).endswith("required") else "failed",
                          "code": str(error)}, sort_keys=True))
        return 2
    print(json.dumps(value, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
