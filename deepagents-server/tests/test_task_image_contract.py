"""An image and its manifest cannot be silently paired across builds."""
from __future__ import annotations

import hashlib
import json
import time
from types import SimpleNamespace

import pytest

from tooling.acceptance.support import image_contract as contract


@pytest.fixture
def build(tmp_path):
    package = tmp_path / "src/mini_loop_deepagents"
    package.mkdir(parents=True)
    (package / "__init__.py").write_text('"""Example package."""\n')
    inputs = {}
    for name in ("pyproject.toml", "uv.lock", "Dockerfile.task", "README.md"):
        (tmp_path / name).write_text(name)
        inputs[name] = hashlib.sha256(name.encode()).hexdigest()
    manifest = {"schema_version": 1, "kind": "mini-loop-task-build-context", "inputs": inputs,
                "wheel_validation": {"source_files": {
                    "mini_loop_deepagents/__init__.py": hashlib.sha256((package / "__init__.py").read_bytes()).hexdigest()}}}
    path = tmp_path / "manifest.json"
    path.write_text(json.dumps(manifest))
    return tmp_path, package, path


def test_manifest_records_actual_digest_and_requires_complete_current_source(build):
    module, package, path = build
    result = contract.validate_manifest(path, module=module)
    assert result["manifest_sha256"] == hashlib.sha256(path.read_bytes()).hexdigest()
    assert len(result["runtime_source_sha256"]) == 1
    (package / "added.py").write_text("NEW = True\n")
    with pytest.raises(contract.HarnessFailure, match="Runtime source"):
        contract.validate_manifest(path, module=module)


def test_manifest_rejects_dependency_or_image_input_drift(build):
    module, _, path = build
    (module / "uv.lock").write_text("changed")
    with pytest.raises(contract.HarnessFailure, match="dependency or image inputs"):
        contract.validate_manifest(path, module=module)


@pytest.mark.parametrize("image", ["task:latest", "sha256:short", "sha256:" + "A" * 64])
def test_image_selection_requires_an_immutable_local_id(image):
    with pytest.raises(contract.HarnessFailure, match="immutable"):
        contract.validate_image_id(image)
    assert contract.validate_image_id("sha256:" + "a" * 64) == "sha256:" + "a" * 64


@pytest.mark.parametrize("digest,matched,code", [("selected", True, 0), ("other", True, 0), ("selected", False, 0), ("selected", True, 1)])
async def test_image_probe_binds_embedded_manifest_and_installed_source(digest, matched, code):
    calls = []
    containers = SimpleNamespace(
        run_id="a" * 32,
        create=lambda *args, **kwargs: (calls.append((args, kwargs)) or {"id": "probe"}),
        owned=lambda _: {"State": {"Running": False, "ExitCode": code, "OOMKilled": False}},
        logs=lambda _: json.dumps({"manifest_sha256": digest, "installed_source_matches_manifest": matched,
                                  "installed_package_files": 3}).encode(),
    )
    processes = SimpleNamespace(check=lambda: None, deadline=time.monotonic() + 20)
    if digest == "selected" and matched and code == 0:
        result = await contract.verify_image_manifest(containers, processes, "selected")
        assert result["container_id"] == "probe" and result["installed_package_files"] == 3
    else:
        with pytest.raises(contract.HarnessFailure):
            await contract.verify_image_manifest(containers, processes, "selected")
    assert calls[0][1] == {"entrypoint": [contract.PYTHON], "transient": True}
    assert calls[0][0][2] == {}  # The image probe receives no deployment secrets.
