"""`python -m mini_loop` -- launch the server with uvicorn.

Env knobs (all optional): HOST, PORT, MINILOOP_FAKE_LLM=1 to run without a key.
"""

from __future__ import annotations

import os

import uvicorn


def main() -> None:
    from .auth import load_auth, refuse_open_bind

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
