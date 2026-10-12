"""Keep the exact failed capture, rather than sampling a replacement observation."""
import hashlib
import json
import sys

import pytest
from tools import export_go_contracts as exporter


def configure(monkeypatch, tmp_path, snapshot, *args):
    target = tmp_path / "target"
    target.mkdir()
    calls = []
    def capture():
        calls.append(True)
        return snapshot
    monkeypatch.setattr(exporter, "TARGET", target)
    monkeypatch.setattr(exporter, "_snapshot", capture)
    monkeypatch.setattr(sys, "argv", ["export_go_contracts.py", *args])
    return target, calls


def test_failed_check_retains_same_observation_without_rewriting_target(monkeypatch, tmp_path, capsys):
    actual = b'{"changed": []}\n'
    diagnostics = tmp_path / "diagnostics"
    target, calls = configure(monkeypatch, tmp_path, {"events.json": actual, "missing.json": b"{}\n"},
                              "--check", "--diagnostics-dir", str(diagnostics))
    expected = b'{"changed": ["source.py"]}\n'
    (target / "events.json").write_bytes(expected)
    assert exporter.main() == 1
    assert calls == [True]
    assert (target / "events.json").read_bytes() == expected
    assert not (target / "missing.json").exists()
    capture, = diagnostics.iterdir()
    assert (capture / "expected/events.json").read_bytes() == expected
    assert (capture / "actual/events.json").read_bytes() == actual
    assert (capture / "actual/missing.json").read_bytes() == b"{}\n"
    assert not (capture / "expected/missing.json").exists()
    manifest = json.loads((capture / "manifest.json").read_bytes())
    assert manifest["snapshot_files"] == 2
    assert manifest["stale"][0]["actual_sha256"] == hashlib.sha256(actual).hexdigest()
    assert manifest["stale"][1]["expected_sha256"] is None
    output = capsys.readouterr().err
    assert '-{"changed": ["source.py"]}' in output
    assert '+{"changed": []}' in output
    assert str(capture) in output


def test_diff_is_bounded_and_corrupt_expected_bytes_are_retained(monkeypatch, tmp_path, capsys):
    actual = ("new\n" * 120).encode()
    diagnostics = tmp_path / "diagnostics"
    target, _ = configure(monkeypatch, tmp_path, {"large.json": actual},
                          "--check", "--diagnostics-dir", str(diagnostics))
    expected = b"\xff\n" + b"old\n" * 120
    (target / "large.json").write_bytes(expected)
    assert exporter.main() == 1
    capture, = diagnostics.iterdir()
    assert (capture / "expected/large.json").read_bytes() == expected
    assert (capture / "actual/large.json").read_bytes() == actual
    output = capsys.readouterr().err
    assert "further diff lines omitted" in output
    assert len(output.splitlines()) <= 85


def test_passing_check_creates_no_diagnostics(monkeypatch, tmp_path):
    diagnostics = tmp_path / "diagnostics"
    target, calls = configure(monkeypatch, tmp_path, {"same.json": b"{}\n"},
                              "--check", "--diagnostics-dir", str(diagnostics))
    (target / "same.json").write_bytes(b"{}\n")
    assert exporter.main() == 0
    assert calls == [True]
    assert not diagnostics.exists()


def test_diagnostics_requires_check_before_capture(monkeypatch, tmp_path):
    diagnostics = tmp_path / "diagnostics"
    _, calls = configure(monkeypatch, tmp_path, {}, "--diagnostics-dir", str(diagnostics))
    with pytest.raises(SystemExit) as error:
        exporter.main()
    assert error.value.code == 2
    assert calls == []
    assert not diagnostics.exists()


def test_capture_retains_compared_expected_bytes_during_target_change(monkeypatch, tmp_path):
    diagnostics = tmp_path / "diagnostics"
    target, _ = configure(monkeypatch, tmp_path, {"changing.json": b"actual\n"},
                          "--check", "--diagnostics-dir", str(diagnostics))
    path = target / "changing.json"
    path.write_bytes(b"compared\n")
    report = exporter._report_export_drift
    def change_target(snapshot, expected, stale, directory):
        path.write_bytes(b"concurrent edit\n")
        report(snapshot, expected, stale, directory)
    monkeypatch.setattr(exporter, "_report_export_drift", change_target)
    assert exporter.main() == 1
    capture, = diagnostics.iterdir()
    assert (capture / "expected/changing.json").read_bytes() == b"compared\n"
    assert path.read_bytes() == b"concurrent edit\n"


@pytest.mark.parametrize("check", [False, True])
def test_success_manifest_hashes_exact_single_capture(monkeypatch, tmp_path, check):
    manifest_path = tmp_path / "nested" / "source.json"
    snapshot = {"python-b.json": b"second\n", "python-a.json": b"first\n"}
    args = ["--manifest", str(manifest_path)] + (["--check"] if check else [])
    target, calls = configure(monkeypatch, tmp_path, snapshot, *args)
    if check:
        for name, body in snapshot.items():
            (target / name).write_bytes(body)
    assert exporter.main() == 0
    assert calls == [True]
    manifest = json.loads(manifest_path.read_bytes())
    assert manifest["files"] == [{"name": name, "sha256": hashlib.sha256(body).hexdigest()}
                                  for name, body in sorted(snapshot.items())]
    assert manifest["producer_sha256"] == hashlib.sha256(exporter.Path(exporter.__file__).read_bytes()).hexdigest()


def test_failed_check_does_not_publish_success_manifest(monkeypatch, tmp_path):
    manifest_path = tmp_path / "source.json"
    configure(monkeypatch, tmp_path, {"missing.json": b"{}\n"},
              "--check", "--manifest", str(manifest_path))
    assert exporter.main() == 1
    assert not manifest_path.exists()
