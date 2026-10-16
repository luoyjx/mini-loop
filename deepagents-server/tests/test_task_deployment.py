"""Offline deployment-boundary checks; no image pull, build or service startup."""

import base64
import csv
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tomllib
import zipfile

import pytest
import yaml

MODULE = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("build_task_bundle", MODULE / "scripts/build_task_bundle.py")
BUNDLE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUNDLE)


def make_wheel(module, destination, *, extra_files=None, omit_extra=False):
    project = tomllib.loads((module / "pyproject.toml").read_text())["project"]
    prefix = f"mini_loop_deepagents-{project['version']}.dist-info/"
    files = BUNDLE.source_files(module)
    metadata = ["Metadata-Version: 2.4", "Name: mini-loop-deepagents", "Version: 0.1.0", "Requires-Python: >=3.12"]
    metadata += [f"Requires-Dist: {value}" for value in project["dependencies"]]
    if not omit_extra:
        metadata += ["Provides-Extra: temporal", "Requires-Dist: temporalio==1.34.0; extra == 'temporal'"]
    files[prefix + "METADATA"] = ("\n".join(metadata) + "\n\n").encode() + (module / "README.md").read_bytes()
    files[prefix + "WHEEL"] = b"Wheel-Version: 1.0\nRoot-Is-Purelib: true\nTag: py3-none-any\nGenerator: test\n"
    files.update(extra_files or {})
    record = io.StringIO()
    writer = csv.writer(record, lineterminator="\n")
    for name, content in files.items():
        digest = base64.urlsafe_b64encode(hashlib.sha256(content).digest()).rstrip(b"=").decode()
        writer.writerow([name, "sha256=" + digest, len(content)])
    writer.writerow([prefix + "RECORD", "", ""])
    files[prefix + "RECORD"] = record.getvalue().encode()
    with zipfile.ZipFile(destination, "w") as archive:
        for name, content in files.items():
            archive.writestr(name, content)
    return destination


@pytest.fixture
def inputs(tmp_path, monkeypatch):
    module = tmp_path / "module"
    source = module / "src/mini_loop_deepagents"
    source.mkdir(parents=True)
    (source / "__init__.py").write_text('"""test package"""\n')
    (module / "README.md").write_text("Current README\n")
    (module / ".env").write_text("PRIVATE_TEST_VALUE=must-not-be-copied\n")
    (module / "pyproject.toml").write_text('''[project]
name = "mini-loop-deepagents"
version = "0.1.0"
requires-python = ">=3.12"
readme = "README.md"
dependencies = ["alpha==1.0"]
[project.optional-dependencies]
temporal = ["temporalio==1.34.0"]
''')
    digest = "a" * 64
    (module / "uv.lock").write_text(f'''version = 1
[[package]]
name = "alpha"
version = "1.0"
source = {{ registry = "https://pypi.org/simple" }}
wheels = [{{ hash = "sha256:{digest}" }}]
[[package]]
name = "temporalio"
version = "1.34.0"
source = {{ registry = "https://pypi.org/simple" }}
wheels = [{{ hash = "sha256:{digest}" }}]
''')
    (module / "Dockerfile.task").write_bytes((MODULE / "Dockerfile.task").read_bytes())
    requirements = f"alpha==1.0 \\\n    --hash=sha256:{digest}\ntemporalio==1.34.0 \\\n    --hash=sha256:{digest}\n".encode()

    def export(command, **kwargs):
        assert "--locked" in command and "--offline" in command
        assert "--no-dev" in command and "--no-emit-project" in command
        assert command[command.index("--extra") + 1] == "temporal"
        return subprocess.CompletedProcess(command, 0, stdout=requirements, stderr=b"")

    monkeypatch.setattr(BUNDLE.subprocess, "run", export)
    wheel = make_wheel(module, tmp_path / "mini_loop_deepagents-0.1.0-py3-none-any.whl")
    return module, wheel, tmp_path / "context", requirements


def test_context_is_allowlisted_and_hashes_match_current_wheel(inputs):
    module, wheel, output, _ = inputs
    manifest = BUNDLE.build_bundle(module, wheel, output)
    files = {p.relative_to(output).as_posix() for p in output.rglob("*") if p.is_file()}
    assert files == {"manifest.json", "Dockerfile", ".dockerignore", "requirements.txt", "wheels/" + wheel.name}
    assert json.loads((output / "manifest.json").read_text()) == manifest
    for name, expected in manifest["files"].items():
        content = (output / name).read_bytes()
        assert BUNDLE.sha256(content) == expected["sha256"]
        assert len(content) == expected["bytes"]
        assert b"must-not-be-copied" not in content
    assert manifest["wheel_validation"]["default_dependencies"] == ["alpha==1.0"]
    assert manifest["wheel_validation"]["optional_dependencies"] == {"temporal": ["temporalio==1.34.0"]}
    again = output.with_name("context-again")
    BUNDLE.build_bundle(module, wheel, again)
    assert (again / "manifest.json").read_bytes() == (output / "manifest.json").read_bytes()


@pytest.mark.parametrize("changed", ["src/mini_loop_deepagents/__init__.py", "README.md"])
def test_stale_wheel_is_rejected_before_creating_context(inputs, changed):
    module, wheel, output, _ = inputs
    (module / changed).write_text("changed since wheel build\n")
    with pytest.raises(ValueError, match="wheel_(source|readme)_mismatch"):
        BUNDLE.build_bundle(module, wheel, output)
    assert not output.exists()


@pytest.mark.parametrize("name", [".env", "../outside.py", "mini_loop_deepagents/private.key", "scripts/entrypoint.py"])
def test_extra_wheel_payloads_are_refused(inputs, name):
    module, wheel, output, _ = inputs
    make_wheel(module, wheel, extra_files={name: b"unexpected"})
    with pytest.raises(ValueError, match="wheel_file_set_mismatch"):
        BUNDLE.build_bundle(module, wheel, output)
    assert not output.exists()


def test_wheel_must_preserve_temporal_extra(inputs):
    module, wheel, output, _ = inputs
    make_wheel(module, wheel, omit_extra=True)
    with pytest.raises(ValueError, match="wheel_optional_dependencies_mismatch"):
        BUNDLE.build_bundle(module, wheel, output)


@pytest.mark.parametrize("bad", [b"alpha>=1.0\n", b"-e .\n", b"--index-url https://example.invalid\n",
                                 b"alpha @ https://example.invalid/a.whl\n", b"alpha==1.0 --hash=sha256:bad\n"])
def test_export_cannot_bypass_lock_and_hashes(inputs, bad):
    module, _, _, _ = inputs
    project = tomllib.loads((module / "pyproject.toml").read_text())["project"]
    lock = tomllib.loads((module / "uv.lock").read_text())
    with pytest.raises(ValueError, match="requirements_"):
        BUNDLE.validate_requirements(bad, lock, project)


def test_export_must_keep_default_dependencies_and_temporal(inputs):
    module, _, _, content = inputs
    project = tomllib.loads((module / "pyproject.toml").read_text())["project"]
    lock = tomllib.loads((module / "uv.lock").read_text())
    with pytest.raises(ValueError, match="requirements_required_dependency_missing"):
        BUNDLE.validate_requirements(content.split(b"temporalio")[0], lock, project)


def test_existing_context_is_never_overwritten(inputs):
    module, wheel, output, _ = inputs
    output.mkdir()
    sentinel = output / "keep"
    sentinel.write_text("unrelated")
    with pytest.raises(ValueError, match="bundle_destination_exists"):
        BUNDLE.build_bundle(module, wheel, output)
    assert sentinel.read_text() == "unrelated"


def test_source_changes_during_export_abort_context(inputs, monkeypatch):
    module, wheel, output, content = inputs

    def export(command, **kwargs):
        (module / "src/mini_loop_deepagents/__init__.py").write_text("changed during export\n")
        return subprocess.CompletedProcess(command, 0, stdout=content, stderr=b"")

    monkeypatch.setattr(BUNDLE.subprocess, "run", export)
    with pytest.raises(ValueError, match="build_inputs_changed"):
        BUNDLE.build_bundle(module, wheel, output)
    assert not output.exists()


def test_profile_separates_roles_and_external_authority():
    profile = yaml.safe_load((MODULE / "compose.task.yaml").read_text())
    assert set(profile["services"]) == {"task-api", "task-worker"}
    api, worker = (profile["services"][name] for name in ("task-api", "task-worker"))
    assert not set(api["secrets"]) & {"openai_key", "temporal_worker_key"}
    assert not set(worker["secrets"]) & {"task_tokens", "task_cursor", "temporal_api_key"}
    assert "OPENAI_API_KEY_FILE" not in api["environment"]
    assert "MINILOOP_DEEPAGENTS_TOKENS_FILE" not in worker["environment"]
    for service in (api, worker):
        assert service["user"] == "10001:10001" and service["read_only"] is True
        assert service["init"] is True and service["stop_grace_period"] == "20s"
        assert service["stop_signal"] == "SIGTERM"
        assert service["cap_drop"] == ["ALL"]
        assert service["pids_limit"] == 128
        assert service["mem_limit"] and service["cpus"]
        assert not set(service) & {"container_name", "ports", "volumes", "env_file", "depends_on", "build"}
        assert service["environment"]["MINILOOP_TASK_TEMPORAL_TLS"] == "true"
        assert all(not key.endswith("API_KEY") for key in service["environment"])
        assert "LANGGRAPH_CLOUD_LICENSE_KEY" not in service["environment"]
    assert "/readyz" in api["healthcheck"]["test"][-1]
    assert "healthcheck" not in worker
    assert "--identity" not in worker["command"]
    assert worker["command"][worker["command"].index("--shutdown-timeout-seconds") + 1] == "15"


def test_image_uses_only_approved_base_and_noneditable_locked_input():
    dockerfile = (MODULE / "Dockerfile.task").read_text()
    bases = [line.split()[1] for line in dockerfile.splitlines() if line.startswith("FROM ")]
    assert bases == [BUNDLE.BASE_IMAGE, BUNDLE.BASE_IMAGE]
    assert "--require-hashes" in dockerfile and "--only-binary=:all:" in dockerfile
    assert "--no-deps wheels/mini_loop_deepagents-*.whl" in dockerfile
    assert "--find-links=/opt/build/wheels" in dockerfile
    assert 'pathlib.Path("manifest.json")' in dockerfile
    assert "USER 10001:10001" in dockerfile
    assert 'ENTRYPOINT ["/opt/venv/bin/python", "-m", "mini_loop_deepagents.task_server.launch"]' in dockerfile
    assert "ADD " not in dockerfile and "COPY . " not in dockerfile
    assert "apt-get" not in dockerfile and "langgraph-api:" not in dockerfile


def audited_dependency(tmp_path, *, wheel_changes=None, corrupt_record=False):
    """A deliberately executable setup.py proves auditing never executes source."""
    prefix = "forbiddenfruit-0.1.4.dist-info/"
    source = {"forbiddenfruit/__init__.py": b"__version__ = '0.1.4'\n",
              "COPYING": b"GPL fixture\n", "COPYING.mit": b"MIT fixture\n", "README.md": b"Fixture README\n",
              "PKG-INFO": b"Metadata-Version: 2.1\nName: forbiddenfruit\nVersion: 0.1.4\n\n",
              "setup.py": b"raise RuntimeError('must never execute')\n"}
    sdist = tmp_path / "forbiddenfruit-0.1.4.tar.gz"
    with tarfile.open(sdist, "w:gz") as archive:
        for name, value in source.items():
            member = tarfile.TarInfo("forbiddenfruit-0.1.4/" + name)
            member.size = len(value)
            archive.addfile(member, io.BytesIO(value))
    files = {"forbiddenfruit/__init__.py": source["forbiddenfruit/__init__.py"],
             prefix + "licenses/COPYING": source["COPYING"], prefix + "licenses/COPYING.mit": source["COPYING.mit"],
             prefix + "METADATA": b"Metadata-Version: 2.4\nName: forbiddenfruit\nVersion: 0.1.4\n\nFixture README\n",
             prefix + "WHEEL": b"Wheel-Version: 1.0\nRoot-Is-Purelib: true\nTag: py3-none-any\nGenerator: untrusted claim\n",
             prefix + "top_level.txt": b"forbiddenfruit\n"}
    files.update(wheel_changes or {})
    record = io.StringIO()
    writer = csv.writer(record, lineterminator="\n")
    for name, content in files.items():
        digest = base64.urlsafe_b64encode(hashlib.sha256(content).digest()).rstrip(b"=").decode()
        writer.writerow([name, "sha256=" + digest, len(content)])
    writer.writerow([prefix + "RECORD", "", ""])
    files[prefix + "RECORD"] = record.getvalue().encode()
    if corrupt_record:
        files[prefix + "RECORD"] = files[prefix + "RECORD"].replace(b"sha256=", b"sha512=", 1)
    wheel = tmp_path / "forbiddenfruit-0.1.4-py3-none-any.whl"
    with zipfile.ZipFile(wheel, "w") as archive:
        for name, value in files.items():
            archive.writestr(name, value)
    package = {"name": "forbiddenfruit", "version": "0.1.4", "source": {"registry": "https://pypi.org/simple"},
               "sdist": {"hash": "sha256:" + BUNDLE.sha256(sdist.read_bytes()), "size": sdist.stat().st_size,
                         "url": "https://example.invalid/forbiddenfruit-0.1.4.tar.gz"}}
    return {"package": [package]}, wheel, sdist


def test_existing_pure_python_wheel_requires_locked_source_bytes(tmp_path):
    lock, wheel, sdist = audited_dependency(tmp_path)
    audit, wheel_bytes = BUNDLE.audit_forbiddenfruit(lock, wheel, sdist)
    assert audit["wheel_sha256"] == BUNDLE.sha256(wheel.read_bytes())
    assert audit["sdist"] == lock["package"][0]["sdist"]
    assert audit["generator_claim"] == "untrusted claim"
    assert len(audit["matched_source_files"]) == 3
    assert wheel_bytes == wheel.read_bytes()
    sdist.write_bytes(sdist.read_bytes() + b"tamper")
    with pytest.raises(ValueError, match="sdist_hash_mismatch"):
        BUNDLE.audit_forbiddenfruit(lock, wheel, sdist)


@pytest.mark.parametrize("name,content", [
    ("forbiddenfruit/__init__.py", b"print('changed runtime')"),
    ("forbiddenfruit-0.1.4.dist-info/licenses/COPYING", b"changed license"),
    ("startup.pth", b"import changed"),
    ("another/__init__.py", b"pass"),
    ("forbiddenfruit-0.1.4.data/scripts/extra", b"#!/bin/sh"),
    ("forbiddenfruit/_extension.so", b"binary"),
    ("forbiddenfruit-0.1.4.dist-info/entry_points.txt", b"[console_scripts]\nextra=another:main"),
    ("forbiddenfruit-0.1.4.dist-info/METADATA", b"Name: forbiddenfruit\nVersion: 0.1.4\nRequires-Dist: extra==1\n\nFixture README\n"),
    ("forbiddenfruit-0.1.4.dist-info/WHEEL", b"Root-Is-Purelib: false\nTag: cp312-cp312-linux_aarch64\n"),
])
def test_audited_wheel_rejects_runtime_or_installer_payload_changes(tmp_path, name, content):
    lock, wheel, sdist = audited_dependency(tmp_path, wheel_changes={name: content})
    with pytest.raises(ValueError):
        BUNDLE.audit_forbiddenfruit(lock, wheel, sdist)


def test_audited_wheel_record_and_explicit_inputs_are_required(tmp_path):
    lock, wheel, sdist = audited_dependency(tmp_path, corrupt_record=True)
    with pytest.raises(ValueError, match="record_mismatch"):
        BUNDLE.audit_forbiddenfruit(lock, wheel, sdist)
    with pytest.raises(ValueError, match="wheel_and_sdist_required"):
        BUNDLE.audit_forbiddenfruit(lock, None, None)


def test_bundle_adds_only_the_audited_hash_and_local_wheel(inputs, tmp_path, monkeypatch):
    module, wheel, output, original = inputs
    lock, dep_wheel, sdist = audited_dependency(tmp_path)
    source = lock["package"][0]["sdist"]
    with (module / "uv.lock").open("a") as stream:
        stream.write('\n[[package]]\nname = "forbiddenfruit"\nversion = "0.1.4"\n'
                     'source = { registry = "https://pypi.org/simple" }\n'
                     f'sdist = {{ hash = "{source["hash"]}", size = {source["size"]}, url = "{source["url"]}" }}\n')
    dependency_line = f"forbiddenfruit==0.1.4 ; implementation_name == 'cpython' \\\n    --hash={source['hash']}\n".encode()
    export = original + dependency_line
    monkeypatch.setattr(BUNDLE.subprocess, "run", lambda command, **kw: subprocess.CompletedProcess(command, 0, export, b""))
    manifest = BUNDLE.build_bundle(module, wheel, output, forbiddenfruit_wheel=dep_wheel, forbiddenfruit_sdist=sdist)
    actual = (output / "requirements.txt").read_bytes()
    assert actual.startswith(original)
    assert source["hash"].encode() in actual
    assert actual.endswith(b"--hash=sha256:" + BUNDLE.sha256(dep_wheel.read_bytes()).encode() + b"\n")
    assert (output / "wheels" / dep_wheel.name).read_bytes() == dep_wheel.read_bytes()
    assert manifest["audited_dependency_wheels"][0]["sdist"] == source
    assert not list(output.rglob("*.tar.gz"))
