"""Fail-closed tests for owned-container accounting and restart evidence."""
from __future__ import annotations

from copy import deepcopy
import json
from pathlib import Path
import sys
from types import SimpleNamespace

import pytest

pytest.importorskip("temporalio")
ROOT = Path(__file__).resolve().parents[1]
from tooling.acceptance.scenarios import containers as scenario
from tooling.acceptance.support import container_checks as harness
support = harness.DOCKER


def frame(stream, content):
    return bytes([stream, 0, 0, 0]) + len(content).to_bytes(4, "big") + content


def test_docker_log_demux_preserves_both_streams_and_rejects_partial_frames():
    raw = frame(1, b"stdout\n") + frame(2, b"stderr\n")
    assert support.decode_stream(raw) == b"stdout\nstderr\n"
    for invalid in (b"short", raw[:-1], b"\x05" + raw[1:], b"\x01\x01" + raw[2:]):
        with pytest.raises(support.DockerFailure):
            support.decode_stream(invalid)


def test_container_takeover_uses_hostname_and_native_identity_not_namespace_pid():
    before = {"workflow_id": "task", "run_id": "run", "activity_id": "4", "step": 3,
              "request_sha256": "same", "attempt": 1, "pid": 1, "hostname": "old", "worker_identity": "old:1"}
    after = {**before, "attempt": 2, "hostname": "new", "worker_identity": "new:1"}
    harness.check_takeover(before, after)
    for field, value in (("hostname", "old"), ("worker_identity", "old:1"), ("attempt", 4), ("request_sha256", "different"), ("run_id", "other")):
        with pytest.raises(harness.HarnessFailure):
            harness.check_takeover(before, {**after, field: value})


def secure_inspect():
    return {"Id": "container", "Config": {"User": "10001:10001", "Hostname": "api", "Env": ["MINILOOP_DEEPAGENTS_TOKENS_FILE=/run/secrets/tokens.json"]},
            "HostConfig": {"ReadonlyRootfs": True, "CapDrop": ["ALL"], "SecurityOpt": ["no-new-privileges:true"],
                "Memory": 192 * support.MIB, "MemorySwap": 192 * support.MIB, "NanoCpus": 250_000_000,
                "PidsLimit": 128, "PortBindings": {"8123/tcp": [{"HostIp": "127.0.0.1", "HostPort": "1234"}]}},
            "Mounts": [{"Type": "bind", "Destination": "/run/secrets/tokens.json", "RW": False}]}


def test_container_contract_rejects_root_rw_cross_role_secret_or_public_binding():
    original = secure_inspect()
    harness.check_container_contract(original, "api", ["/run/secrets/tokens.json"])
    changes = [lambda value: value["Config"].update(User="0"),
               lambda value: value["HostConfig"].update(ReadonlyRootfs=False),
               lambda value: value["HostConfig"].update(MemorySwap=0),
               lambda value: value["HostConfig"].update(PidsLimit=-1),
               lambda value: value["Config"]["Env"].append("OPENAI_API_KEY_FILE=/run/secrets/model.txt"),
               lambda value: value["Mounts"][0].update(RW=True),
               lambda value: value["HostConfig"]["PortBindings"]["8123/tcp"][0].update(HostIp="0.0.0.0")]
    for change in changes:
        value = deepcopy(original)
        change(value)
        with pytest.raises(harness.HarnessFailure):
            harness.check_container_contract(value, "api", ["/run/secrets/tokens.json"])


def test_actual_cgroup_probe_rejects_unlimited_or_different_enforcement():
    contract = {"memory_limit_bytes": 192 * support.MIB, "nano_cpus": 250_000_000, "pids_limit": 128}
    probe = {"uid": 10001, "gid": 10001, "root_write_errno": 30, "tmp_writable": True,
             "tmp_capacity_bytes": 64 * support.MIB,
             "cgroup_v2": {"memory.max": str(192 * support.MIB), "memory.swap.max": "0", "cpu.max": "25000 100000", "pids.max": "128"}}
    harness.check_runtime_probe(probe, contract)
    assert contract["runtime_probe"] == probe
    for field, value in (("memory.max", "max"), ("memory.max", str(256 * support.MIB)),
                         ("memory.swap.max", "max"), ("memory.swap.max", "1"),
                         ("cpu.max", "max 100000"), ("cpu.max", "50000 100000"), ("pids.max", "256")):
        broken = {**probe, "cgroup_v2": {**probe["cgroup_v2"], field: value}}
        with pytest.raises(harness.HarnessFailure, match="cgroup v2"):
            harness.check_runtime_probe(broken, contract)


def test_container_operations_require_both_owned_label_and_exact_id(tmp_path):
    class Engine:
        value = {"Id": "a" * 64, "Name": "/owned", "Config": {"Labels": {support.LABEL: "run"}}}
        def inspect(self, _):
            return self.value
    engine = Engine()
    containers = support.Containers(engine, "image", "run", tmp_path)
    row = {"id": "a" * 64, "name": "owned"}
    assert containers.owned(row) is engine.value
    for value in ({**engine.value, "Id": "b" * 64}, {**engine.value, "Name": "/other"},
                  {**engine.value, "Config": {"Labels": {support.LABEL: "another-run"}}}):
        engine.value = value
        with pytest.raises(support.DockerFailure):
            containers.owned(row)


def test_create_uses_existing_image_limits_readonly_mounts_and_local_port(tmp_path):
    class Engine:
        calls = []
        def request(self, method, route, value=None, **kwargs):
            self.calls.append((method, route, value))
            return {"Id": "a" * 64} if "/create?" in route else None
        def inspect(self, _):
            return {"Id": "a" * 64, "Name": "/owned", "Config": {"Labels": {support.LABEL: "run"}}}
    engine = Engine()
    containers = support.Containers(engine, "sha256:" + "b" * 64, "run", tmp_path)
    containers.create("owned", ["api"], {"TOKENS_FILE": "/run/secrets/tokens"}, [(tmp_path / "tokens", "/run/secrets/tokens")], port=8123)
    value = engine.calls[0][2]
    assert value["Image"].startswith("sha256:") and len(engine.calls) == 2
    assert "platform=linux%2Farm64" in engine.calls[0][1]
    assert value["HostConfig"]["Memory"] == value["HostConfig"]["MemorySwap"] == 192 * support.MIB
    assert value["HostConfig"]["Binds"][0].endswith(":ro")
    assert value["HostConfig"]["PortBindings"]["8123/tcp"][0]["HostIp"] == "127.0.0.1"
    assert all("/images/create" not in route for _, route, _ in engine.calls)


def test_idle_production_worker_requires_ready_sigterm_draining_exit_zero():
    lines = ["TASK_WORKER_READY identity=worker:1", "TASK_WORKER_STOP_REQUESTED signal=SIGTERM",
             "TASK_WORKER_DRAINING", "TASK_WORKER_STOPPED phase=drained"]
    good = "\n".join(lines).encode()
    assert harness.check_idle_shutdown(good, "worker", {"ExitCode": 0, "OOMKilled": False}) == lines
    for blob, state in ((good, {"ExitCode": 137}), (good, {"ExitCode": 0, "OOMKilled": True}),
                        ("\n".join(lines[::-1]).encode(), {"ExitCode": 0}),
                        (good + b"\nTASK_WORKER_HARD_EXIT reason=deadline", {"ExitCode": 0}),
                        (good.replace(b"worker:1", b"worker:2"), {"ExitCode": 0})):
        with pytest.raises(harness.HarnessFailure):
            harness.check_idle_shutdown(blob, "worker", state)


def test_fixture_marker_parser_accepts_only_explicit_structured_markers():
    blob = b'ordinary log\nTASK_CONTAINER_MODEL_GATE {"pid":1,"hostname":"worker-a"}\n'
    assert harness.markers(blob, "MODEL_GATE") == [{"pid": 1, "hostname": "worker-a"}]
    assert harness.markers(blob, "SECURITY") == []
    with pytest.raises(harness.HarnessFailure):
        harness.markers(b"TASK_CONTAINER_MODEL_GATE []\n", "MODEL_GATE")


@pytest.mark.parametrize("oom,host_mib,container_mib", [(False, 1000, 1100), (True, 10, 10)])
def test_budget_counts_container_cgroup_plus_host_rss_and_stops_only_owned(oom, host_mib, container_mib):
    class Failure(RuntimeError):
        pass
    class Blocked(Failure):
        pass
    events = []
    source = SimpleNamespace(HarnessFailure=Failure, ResourceBlocked=Blocked, process_rows=lambda: {},
        owned_rss_kib=lambda *_: host_mib * 1024, free_percent=lambda: 50)
    processes = SimpleNamespace(check=lambda: None, groups=lambda: set(), failure=None,
        stop_all=lambda **_: events.append("host-owned-stop"))
    engine = SimpleNamespace(request=lambda *_: {"memory_stats": {"usage": container_mib * support.MIB},
        "cpu_stats": {"cpu_usage": {"total_usage": 1}}},
        exec=lambda *_: json.dumps({"total": 8000 * support.MIB, "available": 6000 * support.MIB, "psi": "full avg10=0.00"}).encode())
    import threading
    containers = SimpleNamespace(lock=threading.RLock(), records=[{"id": "owned", "expected_running": True, "transient": False}],
        owned=lambda _: {"State": {"Running": True, "OOMKilled": oom}}, engine=engine,
        cleanup=lambda: events.append("containers-owned-cleanup"))
    budget = support.Budget(source, processes, containers)
    budget.observe()
    assert isinstance(processes.failure, Blocked)
    assert events == ["containers-owned-cleanup", "host-owned-stop"]
    if not oom:
        assert budget.samples[0]["aggregate_bytes"] == (host_mib + container_mib) * support.MIB


def test_missing_cli_records_terminal_failure_without_contacting_docker(tmp_path, monkeypatch):
    def forbidden():
        raise AssertionError("Docker should not be contacted after CLI preflight failure")
    monkeypatch.setattr(support, "Engine", forbidden)
    assert scenario.main(["--image", "missing", "--temporal-binary", str(tmp_path / "missing"), "--binary-sha256", "wrong", "--output-dir", str(tmp_path)]) == 1
    path = next(tmp_path.glob("*/receipt.json"))
    receipt = json.loads(path.read_text())
    assert receipt["status"] == "failed" and receipt["cleanup_confirmed"] is True
    assert "containers" not in receipt and path.stat().st_mode & 0o777 == 0o600
