"""`python -m mini_loop` -- launch the server with uvicorn.

Env knobs (all optional): HOST, PORT, MINILOOP_FAKE_LLM=1 to run without a key.
`python -m mini_loop --dump-config` prints the composition that would boot --
settings (redacted), harness seams, tools, posture -- and exits.
"""

from __future__ import annotations

import os
import sys

import uvicorn


def main() -> None:
    from .auth import load_auth, refuse_open_bind

    if "--dump-config" in sys.argv:
        import json

        from .config import build_client, load_settings
        from .identity import dump_config
        from .manager import SessionManager

        cfg = load_settings()
        try:
            client = build_client(cfg)
        except Exception:
            client = None  # composition is inspectable even without a key
        manager = SessionManager(
            cfg, client, enable_features=cfg.enable_features
        )
        print(json.dumps(dump_config(manager, cfg, load_auth()),
                         indent=2, default=str))
        return

    host = os.getenv("HOST", "127.0.0.1")
    refusal = refuse_open_bind(host, load_auth())
    if refusal:
        raise SystemExit(refusal)

    uvicorn.run(
        "mini_loop.server:app",
        host=host,
        port=int(os.getenv("PORT", "8000")),
        reload=bool(os.getenv("MINILOOP_RELOAD")),
    )


if __name__ == "__main__":
    main()
