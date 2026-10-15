"""Small synchronous SSE client. Token is supplied through an environment var."""

import argparse
import os

from langgraph_sdk import get_sync_client


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("message")
    parser.add_argument("--url", default="http://127.0.0.1:2024")
    parser.add_argument("--thread", help="Resume a thread owned by the same user")
    args = parser.parse_args()
    token = os.environ.get("MINILOOP_DEEPAGENTS_CLIENT_TOKEN")
    if not token:
        parser.error("Set MINILOOP_DEEPAGENTS_CLIENT_TOKEN to your bearer token")
    client = get_sync_client(url=args.url, headers={"Authorization": "Bearer " + token})
    thread = args.thread or client.threads.create()["thread_id"]
    print("thread_id:", thread)
    for chunk in client.runs.stream(
        thread, "agent", input={"messages": [{"role": "user", "content": args.message}]},
        stream_mode="values", multitask_strategy="enqueue",
    ):
        if chunk.event == "error":
            raise RuntimeError(chunk.data)
        if chunk.event == "values":
            messages = chunk.data.get("messages", [])
            if messages and messages[-1].get("type") == "ai" and messages[-1].get("content"):
                print(messages[-1]["content"])


if __name__ == "__main__":
    main()
