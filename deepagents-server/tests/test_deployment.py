"""Check production topology contracts without starting Docker or contacting services."""

from pathlib import Path
import re

import yaml


MODULE = Path(__file__).resolve().parents[1]
COMPOSE = yaml.safe_load((MODULE / "compose.yaml").read_text())
SERVICES = COMPOSE["services"]


def default_value(value):
    """Read the sample's Compose fallback without requiring credentials in tests."""
    match = re.fullmatch(r"\$\{[A-Z_]+:-([^}]+)\}", str(value))
    return match[1] if match else str(value)


def test_api_and_workers_share_one_required_immutable_application_image():
    api, worker = SERVICES["api"], SERVICES["worker"]
    assert api["image"] == worker["image"]
    assert api["image"].startswith("${MINILOOP_DEEPAGENTS_IMAGE:?")
    assert "build" not in api and "build" not in worker
    dockerfile = (MODULE / "Dockerfile").read_text()
    assert re.search(r"^FROM langchain/langgraph-api:0\.16\.0-py3\.12@sha256:[0-9a-f]{64}$", dockerfile, re.M)
    assert '"path": "/deps/deepagents-server/src/mini_loop_deepagents/auth.py:auth"' in dockerfile
    assert api["env_file"] == worker["env_file"] == ".env"


def test_api_only_accepts_requests_and_standard_queue_workers_execute_runs():
    api, worker = SERVICES["api"], SERVICES["worker"]
    assert "command" not in api
    assert worker["command"] == ["/storage/queue_entrypoint.sh"]
    assert api["environment"]["N_JOBS_PER_WORKER"] == "0"
    assert int(default_value(worker["environment"]["N_JOBS_PER_WORKER"])) == 2
    assert worker["environment"]["BG_JOB_ISOLATED_LOOPS"] == "true"
    assert int(default_value(worker["environment"]["LANGGRAPH_POSTGRES_POOL_MAX_SIZE"])) // 2 >= 3
    assert api["environment"]["LANGGRAPH_POSTGRES_POOL_MAX_SIZE"] == "10"


def test_api_and_workers_use_same_durable_database_coordination_and_credentials():
    api_env = SERVICES["api"]["environment"]
    worker_env = SERVICES["worker"]["environment"]
    for key in ("DATABASE_URI", "POSTGRES_URI", "REDIS_URI", "LANGSMITH_API_KEY", "LANGGRAPH_CLOUD_LICENSE_KEY"):
        assert api_env[key] == worker_env[key]
    assert api_env["DATABASE_URI"] == api_env["POSTGRES_URI"]
    assert "${POSTGRES_PASSWORD:?" in api_env["DATABASE_URI"]
    assert "${LANGSMITH_API_KEY:?" in api_env["LANGSMITH_API_KEY"]
    assert "${LANGGRAPH_CLOUD_LICENSE_KEY:?" in api_env["LANGGRAPH_CLOUD_LICENSE_KEY"]
    assert SERVICES["postgres"]["volumes"] == ["postgres-data:/var/lib/postgresql/data"]
    assert "postgres-data" in COMPOSE["volumes"]
    for name in ("api", "worker"):
        assert SERVICES[name]["depends_on"] == {
            "postgres": {"condition": "service_healthy"},
            "redis": {"condition": "service_healthy"},
        }


def test_long_job_and_shutdown_windows_are_independent_of_http_requests():
    for name in ("api", "worker"):
        service = SERVICES[name]
        env = service["environment"]
        assert int(default_value(env["BG_JOB_TIMEOUT_SECS"])) == 86400
        assert int(default_value(env["BG_JOB_MAX_RETRIES"])) == 3
        runtime_grace = int(env["BG_JOB_SHUTDOWN_GRACE_PERIOD_SECS"])
        stop_grace = int(service["stop_grace_period"].removesuffix("s"))
        assert runtime_grace == 180
        assert stop_grace >= runtime_grace + 30


def test_backing_services_are_private_and_restart_without_losing_postgres_volume():
    assert SERVICES["api"]["ports"] == ["127.0.0.1:2024:8000"]
    for name, service in SERVICES.items():
        assert service["restart"] == "unless-stopped"
        if name != "api":
            assert "ports" not in service
    for name in ("postgres", "redis"):
        assert re.search(r"@sha256:[0-9a-f]{64}$", SERVICES[name]["image"])
    redis_command = SERVICES["redis"]["command"]
    assert redis_command[redis_command.index("--save") + 1] == ""
    assert redis_command[redis_command.index("--appendonly") + 1] == "no"
    assert "volumes" not in SERVICES["redis"]


def test_example_limits_fit_the_documented_small_host_budget():
    assert {name: (service["cpus"], service["mem_limit"]) for name, service in SERVICES.items()} == {
        "api": (0.5, "512m"),
        "worker": (1, "1g"),
        "postgres": (0.5, "512m"),
        "redis": (0.25, "128m"),
    }
