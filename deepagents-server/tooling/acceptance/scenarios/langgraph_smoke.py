"""Exercise an actual offline Agent Server, including a graceful restart.

Runs in a private temporary cwd, with synthetic credentials and a stateless test
model. Never reads deployment .env or calls a model provider. A watchdog limits
the owned server process group to 1 GiB RSS and stops on macOS memory pressure."""
from __future__ import annotations

import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import httpx

from tooling.paths import MODULE, FIXTURES
from tooling.acceptance.support.langgraph_server import Server


def run():
    log_dir = MODULE.parent / "var/deepagents-delivery"
    log_dir.mkdir(parents=True, exist_ok=True)
    log = log_dir / "http-smoke.log"
    log.write_text("")
    passed = []
    alice_token, bob_token = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
    # Do not inherit provider or tracing credentials into the offline server.
    env = {k: v for k, v in os.environ.items() if not any(x in k for x in ("API_KEY", "TOKEN", "SECRET", "PASSWORD", "MINILOOP_DEEPAGENTS"))}
    env.update(MINILOOP_DEEPAGENTS_TOKENS=json.dumps({alice_token: "alice", bob_token: "bob"}),
               LANGSMITH_TRACING="false", LANGGRAPH_CLI_NO_ANALYTICS="1", OMP_NUM_THREADS="1")
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    with tempfile.TemporaryDirectory(prefix="mini-loop-deepagents-smoke-") as temporary:
        directory = Path(temporary)
        config = directory / "langgraph.json"
        config.write_text(json.dumps({
            "dependencies": [str(MODULE)],
            "graphs": {"agent": str(FIXTURES / "smoke_graph.py") + ":agent"},
            "auth": {"path": str(MODULE / "src/mini_loop_deepagents/auth.py") + ":auth", "disable_studio_auth": True},
        }))
        server = Server(directory, config, env, port, log)
        with httpx.Client(base_url=f"http://127.0.0.1:{port}", timeout=40) as client:
            def request(method, path, *, owner="alice", expected=200, **kwargs):
                headers = {} if owner is None else {"Authorization": "Bearer " + (alice_token if owner == "alice" else bob_token)}
                response = client.request(method, path, headers=headers, **kwargs)
                assert response.status_code == expected, (method, path, response.status_code, response.text[:1000])
                return response

            def tool(thread, name, args, **extra):
                body = {"assistant_id": "agent", "input": {"messages": [{"role": "user", "content": "TOOL:" + json.dumps({"name": name, "args": args})}]}, **extra}
                result = request("POST", f"/threads/{thread}/runs/wait", json=body).json()
                assert "__error__" not in result, result
                return result

            try:
                server.start()
                request("POST", "/threads", owner=None, expected=401, json={})
                thread = request("POST", "/threads", json={"metadata": {"owner": "bob"}}).json()["thread_id"]
                assert request("GET", f"/threads/{thread}").json()["metadata"]["owner"] == "alice"
                request("GET", f"/threads/{thread}", owner="bob", expected=404)
                passed.append("authentication and owner stamping / cross-user read denial")

                state = tool(thread, "write_file", {"file_path": "/note.txt", "content": "thread-private"})
                assert state["files"]["/note.txt"]["content"] == "thread-private"
                second = request("POST", "/threads", json={}).json()["thread_id"]
                empty = tool(second, "read_file", {"file_path": "/note.txt"})
                assert "thread-private" not in empty["messages"][-1]["content"]
                passed.append("real Deep Agents filesystem tools and per-thread checkpoint isolation")

                tool(thread, "write_file", {"file_path": "/memories/preference.txt", "content": "alice-only"})
                memory = tool(second, "read_file", {"file_path": "/memories/preference.txt"},
                              config={"configurable": {"langgraph_auth_user": {"identity": "bob"}, "langgraph_auth_user_id": "bob"}},
                              context={"user_id": "bob", "owner": "bob"})
                assert "alice-only" in memory["messages"][-1]["content"]
                bob = request("POST", "/threads", owner="bob", json={}).json()["thread_id"]
                bob_memory = request("POST", f"/threads/{bob}/runs/wait", owner="bob", json={"assistant_id": "agent", "input": {"messages": [{"role": "user", "content": 'TOOL:{"name":"read_file","args":{"file_path":"/memories/preference.txt"}}'}]}}).json()
                assert "__error__" not in bob_memory and "alice-only" not in bob_memory["messages"][-1]["content"]
                request("POST", f"/threads/{thread}/runs", owner="bob", expected=404, json={"assistant_id": "agent", "input": None})
                request("PATCH", f"/threads/{thread}", owner="bob", expected=404, json={"metadata": {"owner": "bob"}})
                request("DELETE", f"/threads/{thread}", owner="bob", expected=404)
                own = request("POST", "/threads/search", owner="bob", json={}).json()
                assert {x["thread_id"] for x in own} == {bob}
                passed.append("owner memory across threads / spoofed config and context / cross-user mutation denial")

                request("PUT", "/store/items", json={"namespace": ["bob", "memories"], "key": "bad", "value": {}}, expected=403)
                request("PUT", "/store/items", json={"namespace": ["alice", "memories"], "key": "test", "value": {"note": "alice"}}, expected=204)
                request("POST", "/store/items/search", owner="bob", json={"namespace_prefix": ["alice"]}, expected=403)
                passed.append("direct Store API namespace authorization")

                delegated = tool(second, "task", {"description": "Say delegated-ok", "subagent_type": "general-purpose"})
                assert "delegated-ok" in delegated["messages"][-1]["content"]
                passed.append("actual Deep Agents subagent with inherited verified identity")

                with client.stream("POST", f"/threads/{second}/runs/stream", headers={"Authorization": "Bearer " + alice_token},
                                   json={"assistant_id": "agent", "input": {"messages": [{"role": "user", "content": "stream-check"}]}, "stream_mode": ["values"]}) as stream:
                    assert stream.status_code == 200
                    events = "\n".join(stream.iter_lines())
                    assert "event: values" in events and "OFFLINE: stream-check" in events
                passed.append("actual HTTP SSE streaming")

                client_env = {**env, "MINILOOP_DEEPAGENTS_CLIENT_TOKEN": alice_token}
                cli = subprocess.run([str(MODULE / ".venv/bin/python"), str(MODULE / "scripts/chat.py"),
                                      "cli-check", "--url", str(client.base_url).rstrip("/"), "--thread", second],
                                     env=client_env, capture_output=True, text=True, timeout=20)
                assert cli.returncode == 0 and "OFFLINE: cli-check" in cli.stdout, (cli.returncode, cli.stdout, cli.stderr)
                passed.append("documented SDK chat client against actual server")

                active = request("POST", f"/threads/{second}/runs", json={"assistant_id": "agent", "input": {"messages": [{"role": "user", "content": "WAIT:5"}]}}).json()
                run_path = f"/threads/{second}/runs/{active['run_id']}"
                for _ in range(40):
                    if request("GET", run_path).json()["status"] == "running":
                        break
                    time.sleep(0.1)
                else:
                    raise AssertionError("background Run never became active")
                request("POST", f"/threads/{second}/runs", expected=409, json={"assistant_id": "agent", "input": None, "multitask_strategy": "reject"})
                request("POST", run_path + "/cancel?wait=true")
                assert request("GET", run_path).json()["status"] == "interrupted"
                passed.append("same-thread concurrent Run rejection and active cancellation")

                unavailable = tool(second, "execute", {"command": "echo unavailable"})
                assert "execution not available" in unavailable["messages"][-1]["content"].lower()
                passed.append("shell execution refused by virtual backend")

                server.stop()
                server.start()
                restored = request("GET", f"/threads/{thread}/state").json()
                assert restored["values"]["files"]["/note.txt"]["content"] == "thread-private"
                restored_memory = tool(second, "read_file", {"file_path": "/memories/preference.txt"})
                assert "alice-only" in restored_memory["messages"][-1]["content"]
                request("GET", f"/threads/{thread}", owner="bob", expected=404)
                passed.append("graceful dev-server restart restores thread files, owner memory and ownership")
            finally:
                server.stop()
        receipt = {"passed": passed, "peak_server_tree_rss_mib": round(server.peak_rss_kib / 1024, 1), "production_postgres_validation": "not run; licensed deployment is separate"}
        (log_dir / "http-smoke.json").write_text(json.dumps(receipt, indent=2) + "\n")
        print(json.dumps(receipt, indent=2))


if __name__ == "__main__":
    run()
