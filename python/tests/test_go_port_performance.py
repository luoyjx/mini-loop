"""The performance instrument must reject hollow or non-comparable samples."""
import importlib.util
from pathlib import Path
import sys

import pytest


@pytest.fixture
def instrument():
    path = Path(__file__).resolve().parents[1] / "tools" / "go_port_performance.py"
    spec = importlib.util.spec_from_file_location("go_port_performance", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    try:
        spec.loader.exec_module(module)
        yield module
    finally:
        sys.modules.pop(spec.name, None)


def test_nearest_rank_p95_and_invalid_samples(instrument):
    assert instrument.summary(list(range(1, 21))) == {
        "n": 20, "median_ms": 10.5, "p95_ms": 19, "min_ms": 1, "max_ms": 20}
    for values in ([], [-1], [float("nan")], [float("inf")]):
        with pytest.raises(ValueError):
            instrument.summary(values)


def test_environment_cannot_inherit_provider_credentials_or_features(instrument, monkeypatch, tmp_path):
    monkeypatch.setenv("ANTHROPIC_API_KEY", "should-not-inherit")
    monkeypatch.setenv("MINILOOP_FEATURES", "1")
    monkeypatch.setenv("MINILOOP_RELOAD", "1")
    environment = instrument.isolated_environment(tmp_path, 12345)
    assert "ANTHROPIC_API_KEY" not in environment
    assert "MINILOOP_FEATURES" not in environment
    assert "MINILOOP_RELOAD" not in environment
    assert environment["MINILOOP_FAKE_LLM"] == "1"
    assert environment["MINILOOP_FAKE_DELAY"] == "0"
    assert environment["HOME"] == str(tmp_path)
    assert environment["PYTHON_DOTENV_DISABLED"] == "1"


def test_successful_text_without_shell_effect_cannot_count_as_a_turn(instrument, monkeypatch, tmp_path):
    responses = iter([
        {"id": "owned", "workspace": str(tmp_path)},
        {"permission_mode": "auto"},
        {"final": "Done. Tool said: handled: probe"},
    ])
    monkeypatch.setattr(instrument, "request", lambda *args: (next(responses), 1.0))
    with pytest.raises(FileNotFoundError):
        instrument.turn(None, "probe")


def test_empty_workload_refuses_before_constructing_servers(instrument, tmp_path):
    with pytest.raises(SystemExit) as refusal:
        instrument.main(["--go-binary", str(tmp_path / "missing"),
                         "--output", str(tmp_path / "report.json"), "--pairs", "0"])
    assert refusal.value.code == 2
    assert not (tmp_path / "report.json").exists()
