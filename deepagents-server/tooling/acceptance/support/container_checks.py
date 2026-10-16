"""Shared container checks support for bounded acceptance scenarios."""
from __future__ import annotations

import asyncio
import json
import secrets

from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.process import HarnessFailure
from tooling.acceptance.support import containers as DOCKER


PYTHON = "/opt/venv/bin/python"


def markers(blob, kind):
    prefix = "TASK_CONTAINER_" + kind + " "
    result = []
    for line in blob.decode("utf-8", errors="replace").splitlines():
        if line.startswith(prefix):
            value = json.loads(line[len(prefix):])
            if not isinstance(value, dict):
                raise HarnessFailure("Container fixture marker is not an object")
            result.append(value)
    return result


def check_takeover(before, after):
    for field in ("workflow_id", "run_id", "activity_id", "step", "request_sha256"):
        if before.get(field) != after.get(field):
            raise HarnessFailure("Container takeover changed native Activity identity or request at " + field)
    if (after.get("hostname") == before.get("hostname") or after.get("worker_identity") == before.get("worker_identity")
            or not before["attempt"] < after.get("attempt", 0) <= 3):
        raise HarnessFailure("Container takeover lacks a distinct Worker and bounded new attempt")


def check_container_contract(value, role, secret_files):
    config, host = value["Config"], value["HostConfig"]
    ports = host.get("PortBindings") or {}
    env_names = {entry.split("=", 1)[0] for entry in config.get("Env", [])}
    mounted = {item["Destination"] for item in value.get("Mounts", []) if item.get("Type") == "bind"}
    forbidden = ("OPENAI_API_KEY", "OPENAI_API_KEY_FILE") if role == "api" else (
        "MINILOOP_DEEPAGENTS_TOKENS", "MINILOOP_DEEPAGENTS_TOKENS_FILE",
        "MINILOOP_TASK_SERVER_CURSOR_SECRET", "MINILOOP_TASK_SERVER_CURSOR_SECRET_FILE")
    if (config.get("User") != "10001:10001" or not host.get("ReadonlyRootfs") or host.get("CapDrop") != ["ALL"]
            or not {"no-new-privileges", "no-new-privileges:true"}.intersection(host.get("SecurityOpt") or []) or host.get("Memory", 0) <= 0
            or host.get("MemorySwap") != host.get("Memory") or host.get("NanoCpus", 0) <= 0
            or not 1 <= host.get("PidsLimit", 0) <= 128
            or any(binding.get("HostIp") != "127.0.0.1" for bindings in ports.values() for binding in bindings)
            or any(name in env_names for name in forbidden)
            or any(item.get("RW") for item in value.get("Mounts", []) if item.get("Type") == "bind")
            or {path for path in mounted if path.startswith("/run/secrets/")} != set(secret_files)):
        raise HarnessFailure("A Task container violated non-root, readonly, resource or role-secret isolation")
    return {"container_id": value["Id"], "hostname": config["Hostname"], "role": role,
            "vm_pid_at_inspection": value.get("State", {}).get("Pid"), "restart_count": value.get("RestartCount", 0),
            "user": config["User"], "readonly_rootfs": host["ReadonlyRootfs"], "cap_drop": host["CapDrop"],
            "security_options": host["SecurityOpt"], "memory_limit_bytes": host["Memory"], "memory_swap_limit_bytes": host["MemorySwap"],
            "nano_cpus": host["NanoCpus"], "pids_limit": host["PidsLimit"], "tmpfs": host.get("Tmpfs"),
            "published_ports": ports, "secret_mount_destinations": sorted(secret_files),
            "all_bind_mounts_readonly": True, "opposite_role_secret_environment_absent": True}


def check_runtime_probe(value, contract):
    if (value.get("uid") != 10001 or value.get("gid") != 10001 or value.get("root_write_errno") != 30
            or value.get("tmp_writable") is not True or value.get("tmp_capacity_bytes") != 64 * DOCKER.MIB):
        raise HarnessFailure("A Task container failed actual non-root/readonly/tmp write probes")
    limits = value.get("cgroup_v2", {})
    try:
        quota, period = (int(part) for part in limits["cpu.max"].split())
        valid = (int(limits["memory.max"]) == contract["memory_limit_bytes"]
                 and int(limits["memory.swap.max"]) == 0
                 and int(limits["pids.max"]) == contract["pids_limit"]
                 and quota > 0 and period > 0 and quota * 1_000_000_000 == period * contract["nano_cpus"])
    except (KeyError, TypeError, ValueError):
        valid = False
    if not valid:
        raise HarnessFailure("A Task container did not observe its configured hard cgroup v2 limits")
    contract["runtime_probe"] = value


def check_idle_shutdown(blob, hostname, state):
    lines = blob.decode("utf-8", errors="strict").splitlines()
    expected = (f"TASK_WORKER_READY identity={hostname}:1", "TASK_WORKER_STOP_REQUESTED signal=SIGTERM",
                "TASK_WORKER_DRAINING", "TASK_WORKER_STOPPED phase=drained")
    if state.get("ExitCode") != 0 or state.get("OOMKilled") or any(line not in lines for line in expected):
        raise HarnessFailure("The production idle Worker launcher did not become ready and drain successfully on SIGTERM")
    positions = [lines.index(line) for line in expected]
    if positions != sorted(positions) or any(line.startswith("TASK_WORKER_HARD_EXIT") for line in lines):
        raise HarnessFailure("The production idle Worker did not preserve the ready-to-drain lifecycle")
    return list(expected)


SECURITY_PROBE = """import errno,json,os,pathlib
root_errno=None
try:
 f=os.open('/var/tmp/task-image-root-write',os.O_CREAT|os.O_WRONLY,0o600);os.close(f);os.unlink('/var/tmp/task-image-root-write')
except OSError as e:root_errno=e.errno
p=pathlib.Path('/tmp/task-image-write');p.write_text('probe');p.unlink()
s=os.statvfs('/tmp')
limits={name:pathlib.Path('/sys/fs/cgroup',name).read_text().strip() for name in ('memory.max','memory.swap.max','cpu.max','pids.max')}
print(json.dumps({'uid':os.getuid(),'gid':os.getgid(),'root_write_errno':root_errno,'tmp_writable':True,'tmp_capacity_bytes':s.f_blocks*s.f_frsize,'cgroup_v2':limits}))
"""


async def probe_loopback(containers, processes):
    nonce = secrets.token_hex(16).encode()
    received = asyncio.Event()
    async def accept(reader, writer):
        try:
            value = await asyncio.wait_for(reader.readexactly(len(nonce)), timeout=3)
            if value == nonce:
                received.set()
                writer.write(nonce)
                await writer.drain()
        finally:
            writer.close()
            await writer.wait_closed()
    server = await asyncio.start_server(accept, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    code = f"import socket;s=socket.create_connection(('host.docker.internal',{port}),3);s.sendall(bytes.fromhex('{nonce.hex()}'));assert s.recv(64)==bytes.fromhex('{nonce.hex()}');s.close();print('LOOPBACK_PROBE_OK')"
    try:
        probe = containers.create("tc-" + containers.run_id[:12] + "-probe", ["-c", code], {}, entrypoint=[PYTHON], transient=True)
        async def exited():
            return containers.owned(probe)["State"]
        state = await SUPPORT.eventually(processes, exited, lambda value: not value["Running"], timeout=7)
        if state["ExitCode"] != 0 or not received.is_set() or b"LOOPBACK_PROBE_OK" not in containers.logs(probe):
            raise HarnessFailure("host.docker.internal could not reach the owned 127.0.0.1 listener; public rebinding was not attempted")
        return {"container_id": probe["id"], "host_bind": "127.0.0.1", "container_target": "host.docker.internal",
                "nonce_roundtrip": True, "exit_code": state["ExitCode"]}
    finally:
        server.close()
        await server.wait_closed()
