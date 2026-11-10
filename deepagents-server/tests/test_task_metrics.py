"""Task metric taxonomy, safety, and native Prometheus exporter checks."""

from __future__ import annotations

import json
import socket
import subprocess
import sys

import httpx
import pytest

pytest.importorskip("temporalio")

from mini_loop_deepagents.task_server.http import create_app
from mini_loop_deepagents.task_server.metrics import TaskMetrics
from mini_loop_deepagents.task_server.observability import DeploymentIdentity
from mini_loop_deepagents.task_server.telemetry import (
    TaskMetricsConfigurationError, TaskMetricsSettings,
)

TOKEN = "a" * 40
TASK_ID = "task-" + "c" * 64


class Instrument:
    def __init__(self, name):
        self.name = name
        self.values = []

    def add(self, value, attributes=None):
        self.values.append(("add", value, dict(attributes or {})))

    def set(self, value, attributes=None):
        self.values.append(("set", value, dict(attributes or {})))

    def record(self, value, attributes=None):
        self.values.append(("record", value, dict(attributes or {})))


class Meter:
    def __init__(self):
        self.instruments = {}

    def _create(self, name, **kwargs):
        instrument = Instrument(name)
        self.instruments[name] = instrument
        return instrument

    create_counter = _create
    create_gauge = _create
    create_histogram = _create


def test_process_api_activity_and_retry_metrics_are_low_cardinality():
    meter = Meter()
    metrics = TaskMetrics(meter)
    metrics.process_up(True)
    metrics.api_ready(False)
    metrics.worker_state(polling=True, draining=False)
    metrics.api_request("submit", "success")
    metrics.submission("accepted")
    metrics.status("timed_out")
    metrics.cancel("accepted")
    metrics.activity_started(attempt=2, queue_latency_ms=17)
    metrics.activity_finished("failed", attempt=2)
    serialized = json.dumps({name: item.values for name, item in meter.instruments.items()})
    assert "mini_loop_task_process_up" in serialized
    assert '"retry": "true"' in serialized
    assert '"status": "timed_out"' in serialized
    assert '"record", 17' in serialized
    assert not any(name in serialized for name in (TASK_ID, "owner", "message", "credential"))
    inflight = meter.instruments["mini_loop_task_model_activity_inflight"].values
    assert [value for _, value, _ in inflight] == [1, 0]


class Service:
    async def readiness(self):
        return False

    async def submit(self, owner, key, message):
        return {"task_id": TASK_ID, "status": "running", "reused": False}

    async def get(self, owner, task_id):
        return {"task_id": task_id, "status": "timed_out"}

    async def cancel(self, owner, task_id):
        return {"task_id": task_id, "status": "running", "cancel_requested": True}


@pytest.mark.asyncio
async def test_http_outcomes_and_readiness_feed_the_process_meter():
    meter = Meter()
    metrics = TaskMetrics(meter)
    app = create_app(Service(), tokens={TOKEN: "alice"}, metrics=metrics)
    auth = {"Authorization": "Bearer " + TOKEN}
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://task") as client:
        assert (await client.get("/readyz")).status_code == 503
        assert (await client.post("/v1/tasks", headers={**auth, "Idempotency-Key": "key"},
                                  json={"message": "private"})).status_code == 202
        assert (await client.get(f"/v1/tasks/{TASK_ID}", headers=auth)).status_code == 200
        assert (await client.post(f"/v1/tasks/{TASK_ID}/cancel", headers=auth)).status_code == 202
        assert (await client.post("/v1/tasks", headers={**auth, "Idempotency-Key": "key"},
                                  json={"message": " "})).status_code == 400
    values = {name: instrument.values for name, instrument in meter.instruments.items()}
    assert values["mini_loop_task_api_temporal_ready"][-1][1] == 0
    assert ("add", 1, {"outcome": "accepted"}) in values["mini_loop_task_submissions"]
    assert ("add", 1, {"outcome": "rejected"}) in values["mini_loop_task_submissions"]
    assert ("add", 1, {"status": "timed_out"}) in values["mini_loop_task_status_observations"]
    assert ("add", 1, {"outcome": "accepted"}) in values["mini_loop_task_cancel_requests"]


@pytest.mark.parametrize("value", ["localhost:9464", "http://127.0.0.1:9464", "127.0.0.1:0",
                                     "127.0.0.1:65536", "user:secret@127.0.0.1:9464", "[::1]"])
def test_metrics_bind_accepts_only_an_explicit_ip_and_valid_port(value):
    with pytest.raises(TaskMetricsConfigurationError, match="^task_metrics_bind_invalid$"):
        TaskMetricsSettings(value)
    assert TaskMetricsSettings.from_env({}).bind_address is None
    assert TaskMetricsSettings("127.0.0.1:9464").bind_address == "127.0.0.1:9464"
    assert TaskMetricsSettings("[::1]:9464").bind_address == "[::1]:9464"


def test_native_prometheus_endpoint_is_machine_readable_without_an_extra_dependency():
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    code = f'''\
import urllib.request
from mini_loop_deepagents.task_server.observability import DeploymentIdentity
from mini_loop_deepagents.task_server.telemetry import TaskMetricsSettings, create_metrics_runtime
runtime, metrics = create_metrics_runtime("api", DeploymentIdentity.unavailable(), TaskMetricsSettings("127.0.0.1:{port}"))
metrics.submission("accepted")
print(urllib.request.urlopen("http://127.0.0.1:{port}/metrics", timeout=2).read().decode())
'''
    completed = subprocess.run([sys.executable, "-c", code], check=True, capture_output=True, text=True, timeout=10)
    assert 'mini_loop_task_submissions_total{outcome="accepted"' in completed.stdout
    assert 'mini_loop_service_role="api"' in completed.stdout
    assert "mini_loop_task_process_up" in completed.stdout
