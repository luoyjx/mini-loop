"""Owned Docker Engine API operations and resource accounting for acceptance only."""
from __future__ import annotations

import concurrent.futures
import http.client
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import threading
import time
from urllib.parse import quote, urlencode

MIB = 1024 * 1024
LABEL = "mini-loop.task-container-acceptance"


class DockerFailure(RuntimeError):
    pass


class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path, timeout):
        super().__init__("localhost", timeout=timeout)
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.path)


def decode_stream(raw):
    """Docker non-TTY stdout/stderr multiplexing; reject truncated frames."""
    output = bytearray()
    while raw:
        if len(raw) < 8 or raw[0] not in (0, 1, 2) or raw[1:4] != b"\x00\x00\x00":
            raise DockerFailure("docker_log_frame_invalid")
        size = int.from_bytes(raw[4:8], "big")
        if len(raw) < 8 + size:
            raise DockerFailure("docker_log_frame_truncated")
        output.extend(raw[8:8 + size])
        raw = raw[8 + size:]
    return bytes(output)


class Engine:
    def __init__(self, socket_path=None):
        if socket_path is None:
            result = subprocess.run(["docker", "context", "inspect", "--format", "{{json .Endpoints.docker.Host}}"],
                                    capture_output=True, text=True, timeout=5, check=False)
            if result.returncode:
                raise DockerFailure("docker_context_unavailable")
            endpoint = json.loads(result.stdout)
            if not isinstance(endpoint, str) or not endpoint.startswith("unix://"):
                raise DockerFailure("only_local_unix_docker_context_supported")
            socket_path = endpoint.removeprefix("unix://")
        self.socket_path = str(socket_path)

    def request(self, method, route, value=None, *, expected=(200,), raw=False, timeout=3, maximum=8 * MIB):
        connection = UnixConnection(self.socket_path, timeout)
        try:
            body = None if value is None else json.dumps(value).encode()
            connection.request(method, route, body=body, headers={} if body is None else {"Content-Type": "application/json"})
            response = connection.getresponse()
            content = response.read(maximum + 1)
            if len(content) > maximum:
                raise DockerFailure("docker_response_limit_exceeded")
            if response.status not in expected:
                # Docker errors can include deployment arguments. Never reproduce them.
                raise DockerFailure(f"docker_operation_http_{response.status}")
            if raw:
                return content
            return json.loads(content) if content else None
        finally:
            connection.close()

    def inspect(self, identifier):
        return self.request("GET", "/containers/" + quote(identifier, safe="") + "/json")

    def exec(self, identifier, arguments):
        created = self.request("POST", f"/containers/{identifier}/exec",
                               {"AttachStdout": True, "AttachStderr": True, "Tty": False, "Cmd": arguments}, expected=(201,))
        raw = self.request("POST", f"/exec/{created['Id']}/start", {"Detach": False, "Tty": False}, raw=True, timeout=5)
        result = self.request("GET", f"/exec/{created['Id']}/json")
        if result.get("Running") or result.get("ExitCode") != 0:
            raise DockerFailure("docker_probe_exec_failed")
        return decode_stream(raw)


class Containers:
    def __init__(self, engine, image, run_id, directory):
        self.engine, self.image, self.run_id, self.directory = engine, image, run_id, directory
        self.records = []
        self.lock = threading.RLock()
        self.cleanup_lock = threading.Lock()

    def owned(self, record):
        value = self.engine.inspect(record["id"] or record["name"])
        if value["Config"].get("Labels", {}).get(LABEL) != self.run_id or value["Name"].lstrip("/") != record["name"]:
            raise DockerFailure("refusing_unowned_container_operation")
        if record["id"] is not None and value["Id"] != record["id"]:
            raise DockerFailure("container_identity_changed")
        record["id"] = value["Id"]
        return value

    def create(self, name, command, environment, mounts=(), *, port=None, worker=False, entrypoint=None, transient=False):
        record = {"name": name, "id": None, "expected_running": False, "transient": transient,
                  "role": "worker" if worker else "api" if port else "probe"}
        with self.lock:
            self.records.append(record)  # Name is retained even after an ambiguous create response.
        memory = (384 if worker else 192 if port else 64) * MIB
        configuration = {"Image": self.image, "Hostname": name, "User": "10001:10001", "WorkingDir": "/opt/task",
            "Labels": {LABEL: self.run_id}, "Cmd": list(map(str, command)),
            "Env": [f"{key}={value}" for key, value in {"HOME": "/tmp", "PYTHONDONTWRITEBYTECODE": "1", "PYTHONUNBUFFERED": "1", **environment}.items()],
            "HostConfig": {"ReadonlyRootfs": True, "Memory": memory, "MemorySwap": memory,
                "NanoCpus": 500_000_000 if worker else 250_000_000, "PidsLimit": 128,
                "CapDrop": ["ALL"], "SecurityOpt": ["no-new-privileges:true"], "NetworkMode": "bridge",
                "Tmpfs": {"/tmp": "rw,nosuid,nodev,noexec,size=67108864,mode=1777"},
                "Binds": [f"{Path(source).resolve()}:{target}:ro" for source, target in mounts],
                "LogConfig": {"Type": "json-file", "Config": {"max-size": "1m", "max-file": "1"}}}}
        if entrypoint is not None:
            configuration["Entrypoint"] = entrypoint
        if port is not None:
            configuration["ExposedPorts"] = {"8123/tcp": {}}
            configuration["HostConfig"]["PortBindings"] = {"8123/tcp": [{"HostIp": "127.0.0.1", "HostPort": str(port)}]}
        created = self.engine.request("POST", "/containers/create?" + urlencode({"name": name, "platform": "linux/arm64"}), configuration, expected=(201,))
        if not re.fullmatch(r"[0-9a-f]{64}", created["Id"]):
            raise DockerFailure("docker_container_id_invalid")
        record["id"] = created["Id"]
        self.owned(record)
        self.engine.request("POST", f"/containers/{record['id']}/start", expected=(204,))
        record["expected_running"] = not transient
        return record

    def logs(self, record):
        self.owned(record)
        raw = self.engine.request("GET", f"/containers/{record['id']}/logs?stdout=1&stderr=1", raw=True)
        return decode_stream(raw)

    def stop(self, record, *, kill=False, grace_seconds=1):
        if not isinstance(grace_seconds, int) or not 1 <= grace_seconds <= 20:
            raise DockerFailure("invalid_container_stop_grace")
        with self.lock:
            record["expected_running"] = False
        value = self.owned(record)
        if value["State"]["Running"]:
            route = f"/containers/{record['id']}/kill?signal=SIGKILL" if kill else f"/containers/{record['id']}/stop?t={grace_seconds}"
            self.engine.request("POST", route, expected=(204, 304), timeout=3 if kill else grace_seconds + 2)
        value = self.owned(record)
        deadline = time.monotonic() + 2
        while value["State"]["Running"] and time.monotonic() < deadline:
            time.sleep(0.05)
            value = self.owned(record)
        if value["State"]["Running"]:
            raise DockerFailure("owned_container_did_not_stop")
        record["terminal_state"] = value["State"]
        return value

    def cleanup(self):
        with self.cleanup_lock:
            def remove(record):
                try:
                    self.stop(record, kill=True)
                    data = self.logs(record)
                    path = self.directory / (record["name"] + ".log")
                    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "wb") as stream:
                        stream.write(data)
                    record["log"] = str(path)
                    self.engine.request("DELETE", f"/containers/{record['id']}?force=1&v=1", expected=(204,))
                    record["removed"] = True
                except DockerFailure as error:
                    if str(error) == "docker_operation_http_404":
                        record["removed"] = True
                    else:
                        record["cleanup_error"] = type(error).__name__
                except Exception as error:
                    record["cleanup_error"] = type(error).__name__
            with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
                list(pool.map(remove, list(self.records)))
            try:
                remains = self.engine.request("GET", "/containers/json?" + urlencode({"all": "1", "filters": json.dumps({"label": [LABEL + "=" + self.run_id]})}))
                return not remains and all(row.get("removed") for row in self.records)
            except Exception:
                return False


VM_PROBE = """import json,pathlib
m={k:int(v.strip().split()[0])*1024 for k,v in (x.split(':',1) for x in pathlib.Path('/proc/meminfo').read_text().splitlines())}
p=pathlib.Path('/proc/pressure/memory')
print(json.dumps({'total':m['MemTotal'],'available':m['MemAvailable'],'psi':p.read_text() if p.exists() else None}))
"""


class Budget:
    def __init__(self, support, processes, containers):
        self.support, self.processes, self.containers = support, processes, containers
        self.samples = []
        self.stopped = threading.Event()
        self.thread = threading.Thread(target=self.observe, name="container-acceptance-budget", daemon=True)

    def observe(self):
        failures = 0
        vm = None
        last_vm = 0
        while not self.stopped.is_set():
            try:
                self.processes.check()
                rows = self.support.process_rows()
                host = self.support.owned_rss_kib(rows, os.getpid(), self.processes.groups()) * 1024
                free = self.support.free_percent()
                memory, usage, candidates = 0, [], []
                with self.containers.lock:
                    records = list(self.containers.records)
                for record in records:
                    if not record["id"] or record.get("removed"):
                        continue
                    state = self.containers.owned(record)["State"]
                    if state.get("OOMKilled"):
                        raise self.support.ResourceBlocked("An owned container was OOM-killed")
                    if record["expected_running"] and not state["Running"]:
                        raise self.support.HarnessFailure("An expected running container exited")
                    if state["Running"]:
                        stats = self.containers.engine.request("GET", f"/containers/{record['id']}/stats?stream=false&one-shot=true")
                        current = stats.get("memory_stats", {}).get("usage")
                        if not isinstance(current, int) or current <= 0:
                            raise DockerFailure("running_container_cgroup_memory_unavailable")
                        memory += current
                        usage.append({"id": record["id"], "memory_current_bytes": current,
                                      "cpu_total_ns": stats.get("cpu_stats", {}).get("cpu_usage", {}).get("total_usage")})
                        if not record["transient"]:
                            candidates.append(record)
                if candidates and time.monotonic() - last_vm > 2:
                    vm = json.loads(self.containers.engine.exec(candidates[0]["id"], ["/opt/venv/bin/python", "-c", VM_PROBE]))
                    last_vm = time.monotonic()
                sample = {"monotonic_ns": time.monotonic_ns(), "host_owned_rss_bytes": host, "container_cgroup_bytes": memory,
                          "aggregate_bytes": host + memory, "host_free_percent": free, "containers": usage, "vm": vm}
                self.samples.append(sample)
                if host + memory > 2048 * MIB:
                    raise self.support.ResourceBlocked("Owned host RSS plus container cgroup memory exceeded 2 GiB")
                if free is not None and free < 15:
                    raise self.support.ResourceBlocked("Host memory free percentage fell below 15 percent")
                if vm is not None and vm["available"] / vm["total"] < 0.15:
                    raise self.support.ResourceBlocked("Docker VM MemAvailable fell below 15 percent")
                failures = 0
            except Exception as error:
                failures += 1
                if isinstance(error, (self.support.ResourceBlocked, self.support.HarnessFailure)) or failures >= 3:
                    self.processes.failure = error if isinstance(error, self.support.HarnessFailure) else self.support.ResourceBlocked("Container resource observation repeatedly failed")
                    self.containers.cleanup()
                    self.processes.stop_all(force=True)
                    return
            self.stopped.wait(0.5)

    def start(self):
        self.thread.start()

    def stop(self):
        self.stopped.set()
        self.thread.join(timeout=5)
