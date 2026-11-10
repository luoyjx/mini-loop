"""CLI-owned Prometheus runtime configuration for Task API and Worker roles."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
import ipaddress
import os
from urllib.parse import urlsplit

from temporalio.runtime import PrometheusConfig, Runtime, TelemetryConfig

from .metrics import NOOP_TASK_METRICS, TaskMetrics
from .observability import DeploymentIdentity

METRICS_BIND_ENV = "MINILOOP_TASK_METRICS_BIND"


class TaskMetricsConfigurationError(ValueError):
    """Fixed startup code that does not echo ambient configuration."""


@dataclass(frozen=True)
class TaskMetricsSettings:
    bind_address: str | None = None

    def __post_init__(self) -> None:
        if self.bind_address is None:
            return
        try:
            value = self.bind_address
            if (not isinstance(value, str) or len(value) > 128 or not value.isascii()
                    or any(ord(char) <= 32 or char in "/?#@\\" for char in value)):
                raise ValueError()
            parsed = urlsplit("//" + value)
            if (parsed.hostname is None or parsed.port is None or not 1 <= parsed.port <= 65535
                    or parsed.username is not None or parsed.password is not None
                    or parsed.path or parsed.query or parsed.fragment):
                raise ValueError()
            host = parsed.hostname
            if "%" in host:
                raise ValueError()
            ipaddress.ip_address(host)
        except (TypeError, ValueError):
            raise TaskMetricsConfigurationError("task_metrics_bind_invalid") from None

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> TaskMetricsSettings:
        env = os.environ if environ is None else environ
        return cls(bind_address=env.get(METRICS_BIND_ENV))


def create_metrics_runtime(
    role: str, deployment: DeploymentIdentity, settings: TaskMetricsSettings,
) -> tuple[Runtime | None, TaskMetrics]:
    """Create one native exporter before connecting the role's Temporal Client."""
    if role not in {"api", "worker"}:
        raise TaskMetricsConfigurationError("task_metrics_role_invalid") from None
    if settings.bind_address is None:
        return None, NOOP_TASK_METRICS
    try:
        runtime = Runtime(telemetry=TelemetryConfig(
            metrics=PrometheusConfig(
                bind_address=settings.bind_address,
                counters_total_suffix=True,
                unit_suffix=True,
                durations_as_seconds=True,
            ),
            global_tags={
                "mini_loop_service_role": role,
                "mini_loop_protocol_version": deployment.protocol_version,
                "mini_loop_snapshot_schema": str(deployment.snapshot_schema),
                "mini_loop_image_digest": deployment.image_digest,
            },
        ))
    except Exception:
        raise TaskMetricsConfigurationError("task_metrics_start_failed") from None
    metrics = TaskMetrics(runtime.metric_meter)
    metrics.process_up(True)
    if role == "worker":
        metrics.worker_state(polling=False, draining=False)
    else:
        metrics.api_ready(False)
    return runtime, metrics
