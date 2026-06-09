"""`python -m mini_loop` -- launch the server with uvicorn.

Env knobs (all optional): HOST, PORT, MINILOOP_FAKE_LLM=1 to run without a key.
"""

from __future__ import annotations

import os

import uvicorn


def main() -> None:
    uvicorn.run(
        "mini_loop.server:app",
        host=os.getenv("HOST", "127.0.0.1"),
        port=int(os.getenv("PORT", "8000")),
        reload=bool(os.getenv("MINILOOP_RELOAD")),
    )


if __name__ == "__main__":
    main()
