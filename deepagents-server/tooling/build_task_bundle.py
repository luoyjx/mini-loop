"""Create an allowlisted Task image context from a locked project and current wheel.

This uses only Python's standard library and the existing uv executable. It does
not build wheels, install packages, contact registries or invoke Docker. Build a
fresh project wheel first; every packaged file and README must match the checkout.
"""

from __future__ import annotations

import argparse
import base64
import csv
from email.parser import BytesParser
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import tomllib
import zipfile

from tooling.paths import MODULE
BASE_IMAGE = "python:3.12.15-slim-bookworm@sha256:34386ef0cb081344d7ec1c103ba398e6e9f64e9ab3a1509accc92a4e24a07258"
MAX_WHEEL_BYTES = 32 * 1024 * 1024
MAX_REQUIREMENTS_BYTES = 8 * 1024 * 1024
REQUIREMENT = re.compile(r"([a-zA-Z0-9_.-]+)(?:\[[a-zA-Z0-9_,.-]+\])?==([^\s;]+)(?:\s*;[^\\]*)?\Z")


def sha256(content: bytes) -> str:
    return hashlib.sha256(content).hexdigest()


def normalize_name(name: str) -> str:
    return re.sub(r"[-_.]+", "-", name).lower()


def validate_records(content: dict[str, bytes], record_name: str) -> None:
    records = list(csv.reader(io.StringIO(content[record_name].decode())))
    if (len(records) != len(content) or any(len(row) != 3 for row in records)
            or {row[0] for row in records} != set(content)):
        raise ValueError("wheel_record_invalid")
    for name, digest, size in records:
        if name == record_name:
            if digest or size:
                raise ValueError("wheel_record_invalid")
            continue
        value = content[name]
        expected = "sha256=" + base64.urlsafe_b64encode(hashlib.sha256(value).digest()).rstrip(b"=").decode()
        if digest != expected or size != str(len(value)):
            raise ValueError("wheel_record_mismatch")


def audit_forbiddenfruit(lock: dict, wheel: Path | None, sdist: Path | None) -> tuple[dict, bytes]:
    """One existing pure-Python dependency; never execute or build its sdist.

    A cached wheel's filename or generator is not provenance. Its only runtime
    module and both license files must equal the archive authenticated by uv.lock.
    """
    packages = [p for p in lock["package"] if p["name"] == "forbiddenfruit"]
    if not packages and wheel is None and sdist is None:
        return {}, b""
    if len(packages) != 1 or wheel is None or sdist is None:
        raise ValueError("forbiddenfruit_audited_wheel_and_sdist_required")
    package = packages[0]
    if (package["version"] != "0.1.4" or package.get("wheels")
            or package.get("source") != {"registry": "https://pypi.org/simple"}):
        raise ValueError("forbiddenfruit_lock_contract_changed")
    for path in (wheel, sdist):
        if path.is_symlink() or not path.is_file() or not 0 < path.stat().st_size <= 1024 * 1024:
            raise ValueError("forbiddenfruit_input_invalid")
    if wheel.name != "forbiddenfruit-0.1.4-py3-none-any.whl":
        raise ValueError("forbiddenfruit_wheel_name_invalid")
    source_bytes, wheel_bytes = sdist.read_bytes(), wheel.read_bytes()
    source_record = package["sdist"]
    if ("sha256:" + sha256(source_bytes) != source_record["hash"]
            or len(source_bytes) != source_record["size"]):
        raise ValueError("forbiddenfruit_sdist_hash_mismatch")
    prefix = "forbiddenfruit-0.1.4/"
    with tarfile.open(fileobj=io.BytesIO(source_bytes), mode="r:gz") as archive:
        members = archive.getmembers()
        if len(members) > 100 or sum(m.size for m in members) > 4 * 1024 * 1024:
            raise ValueError("forbiddenfruit_sdist_size_invalid")
        if len({m.name for m in members}) != len(members):
            raise ValueError("forbiddenfruit_sdist_duplicate_member")
        needed = ("forbiddenfruit/__init__.py", "COPYING", "COPYING.mit", "PKG-INFO", "README.md")
        source = {}
        for name in needed:
            member = archive.getmember(prefix + name)
            if not member.isfile():
                raise ValueError("forbiddenfruit_sdist_file_invalid")
            source[name] = archive.extractfile(member).read()
    dist = "forbiddenfruit-0.1.4.dist-info/"
    allowed = {"forbiddenfruit/__init__.py"} | {
        dist + name for name in ("METADATA", "WHEEL", "RECORD", "top_level.txt", "licenses/COPYING", "licenses/COPYING.mit")}
    with zipfile.ZipFile(io.BytesIO(wheel_bytes)) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)) or set(names) != allowed:
            raise ValueError("forbiddenfruit_wheel_file_set_mismatch")
        if sum(item.file_size for item in archive.infolist()) > 1024 * 1024:
            raise ValueError("forbiddenfruit_wheel_size_invalid")
        content = {name: archive.read(name) for name in names}
    matched = {"forbiddenfruit/__init__.py": source["forbiddenfruit/__init__.py"],
               dist + "licenses/COPYING": source["COPYING"], dist + "licenses/COPYING.mit": source["COPYING.mit"]}
    if any(content[name] != value for name, value in matched.items()):
        raise ValueError("forbiddenfruit_source_mismatch")
    metadata = BytesParser().parsebytes(content[dist + "METADATA"])
    source_metadata = BytesParser().parsebytes(source["PKG-INFO"])
    for key in ("Name", "Version", "Requires-Dist", "Requires-Python", "Provides-Extra"):
        if sorted(metadata.get_all(key, [])) != sorted(source_metadata.get_all(key, [])):
            raise ValueError("forbiddenfruit_metadata_mismatch")
    if metadata.get_all("Name") != ["forbiddenfruit"] or metadata.get_all("Version") != ["0.1.4"]:
        raise ValueError("forbiddenfruit_metadata_mismatch")
    if (content[dist + "top_level.txt"] != b"forbiddenfruit\n"
            or content[dist + "METADATA"].partition(b"\n\n")[2].rstrip(b"\n") != source["README.md"].rstrip(b"\n")):
        raise ValueError("forbiddenfruit_metadata_mismatch")
    wheel_metadata = BytesParser().parsebytes(content[dist + "WHEEL"])
    if wheel_metadata.get_all("Root-Is-Purelib") != ["true"] or wheel_metadata.get_all("Tag") != ["py3-none-any"]:
        raise ValueError("forbiddenfruit_wheel_platform_invalid")
    validate_records(content, dist + "RECORD")
    return {"name": "forbiddenfruit", "version": "0.1.4", "filename": wheel.name,
            "sdist": source_record, "wheel_sha256": sha256(wheel_bytes), "wheel_bytes": len(wheel_bytes),
            "matched_source_files": {name: sha256(value) for name, value in matched.items()},
            "generator_claim": wheel_metadata["Generator"],
            "verification": "locked sdist hash; exact runtime and license bytes; metadata and full RECORD; no source execution"}, wheel_bytes


def add_audited_wheel_hash(requirements: bytes, audited: dict) -> bytes:
    if not audited:
        return requirements
    pattern = rb"(?m)^(forbiddenfruit==0\.1\.4[^\n]*)(\n(?:[ \t]+--hash=[^\n]*\n?)+)"
    def append(match):
        original = match.group(0).rstrip(b"\n")
        return original + b" \\\n    --hash=sha256:" + audited["wheel_sha256"].encode() + b"\n"
    result, count = re.subn(pattern, append, requirements)
    if count != 1:
        raise ValueError("forbiddenfruit_export_contract_changed")
    return result


def source_files(module: Path) -> dict[str, bytes]:
    """Only the wheel's declared source package, never checkout environment files."""
    root = module / "src"
    files = {}
    for path in sorted((root / "mini_loop_deepagents").rglob("*")):
        if "__pycache__" in path.parts or path.suffix in {".pyc", ".pyo"}:
            continue
        if path.is_symlink():
            raise ValueError("source_symlink_refused")
        if path.is_file():
            if path.name.startswith(".env") or path.suffix in {".key", ".pem", ".db", ".sqlite", ".sqlite3"}:
                raise ValueError("source_sensitive_file_refused")
            files[path.relative_to(root).as_posix()] = path.read_bytes()
    if not files:
        raise ValueError("source_package_missing")
    return files


def validate_wheel(module: Path, wheel: Path, project: dict) -> dict:
    """Prove wheel source, metadata, hashes and file set before copying it."""
    if wheel.is_symlink() or not wheel.is_file() or not 0 < wheel.stat().st_size <= MAX_WHEEL_BYTES:
        raise ValueError("wheel_file_invalid")
    if not re.fullmatch(r"mini_loop_deepagents-[A-Za-z0-9_.]+-py3-none-any\.whl", wheel.name):
        raise ValueError("wheel_name_invalid")
    expected = source_files(module)
    dist = "mini_loop_deepagents-" + project["version"] + ".dist-info/"
    allowed = set(expected) | {dist + suffix for suffix in ("METADATA", "WHEEL", "RECORD")}
    with zipfile.ZipFile(wheel) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)) or set(names) != allowed:
            raise ValueError("wheel_file_set_mismatch")
        if sum(item.file_size for item in archive.infolist()) > MAX_WHEEL_BYTES:
            raise ValueError("wheel_expanded_size_exceeded")
        content = {name: archive.read(name) for name in names}
    if any(content[name] != value for name, value in expected.items()):
        raise ValueError("wheel_source_mismatch")
    metadata = BytesParser().parsebytes(content[dist + "METADATA"])
    if (normalize_name(metadata["Name"]) != normalize_name(project["name"])
            or metadata["Version"] != project["version"]
            or metadata["Requires-Python"] != project["requires-python"]):
        raise ValueError("wheel_metadata_mismatch")
    if content[dist + "METADATA"].partition(b"\n\n")[2] != (module / project["readme"]).read_bytes():
        raise ValueError("wheel_readme_mismatch")
    requirements = metadata.get_all("Requires-Dist", [])
    if sorted(r for r in requirements if ";" not in r) != sorted(project["dependencies"]):
        raise ValueError("wheel_default_dependencies_mismatch")
    extras = project["optional-dependencies"]
    expected_extras = sorted(f"{requirement};extra=={extra}" for extra, values in extras.items() for requirement in values)
    actual_extras = sorted(re.sub(r"[\s'\"]", "", r) for r in requirements if ";" in r)
    if actual_extras != expected_extras or sorted(metadata.get_all("Provides-Extra", [])) != sorted(extras):
        raise ValueError("wheel_optional_dependencies_mismatch")
    wheel_metadata = BytesParser().parsebytes(content[dist + "WHEEL"])
    if wheel_metadata["Root-Is-Purelib"] != "true" or wheel_metadata.get_all("Tag") != ["py3-none-any"]:
        raise ValueError("wheel_platform_invalid")
    validate_records(content, dist + "RECORD")
    return {"source_files": {name: sha256(value) for name, value in expected.items()},
            "wheel_sha256": sha256(wheel.read_bytes()),
            "default_dependencies": sorted(project["dependencies"]),
            "optional_dependencies": extras, "wheel_generator": wheel_metadata["Generator"]}


def validate_requirements(content: bytes, lock: dict, project: dict) -> None:
    """Refuse unpinned, unhashed, URL, editable or installer-option requirements."""
    if not 0 < len(content) <= MAX_REQUIREMENTS_BYTES:
        raise ValueError("requirements_size_invalid")
    locked = {(normalize_name(p["name"]), p["version"]): p for p in lock["package"]}
    logical = content.decode("utf-8").replace("\\\n", " ")
    seen = set()
    for line in logical.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        pieces = line.split("--hash=")
        match = REQUIREMENT.fullmatch(pieces[0].strip())
        if match is None or len(pieces) < 2:
            raise ValueError("requirements_unpinned_or_unhashed")
        key = (normalize_name(match[1]), match[2])
        package = locked.get(key)
        if package is None or package.get("source") != {"registry": "https://pypi.org/simple"}:
            raise ValueError("requirements_not_from_locked_registry")
        hashes = {item["hash"] for item in package.get("wheels", [])}
        if "sdist" in package:
            hashes.add(package["sdist"]["hash"])
        if any(piece.strip() not in hashes for piece in pieces[1:]):
            raise ValueError("requirements_hash_mismatch")
        seen.add(key[0])
    required = project["dependencies"] + project["optional-dependencies"]["temporal"]
    if not {normalize_name(re.split(r"[\[=<>!~; ]", item)[0]) for item in required} <= seen:
        raise ValueError("requirements_required_dependency_missing")


def build_bundle(module: Path, wheel: Path, output: Path, uv: str = "uv", *,
                 forbiddenfruit_wheel: Path | None = None, forbiddenfruit_sdist: Path | None = None) -> dict:
    module = module.resolve()
    if output.exists() or output.is_symlink():
        raise ValueError("bundle_destination_exists")
    project_bytes = (module / "pyproject.toml").read_bytes()
    lock_bytes = (module / "uv.lock").read_bytes()
    project = tomllib.loads(project_bytes.decode())["project"]
    lock = tomllib.loads(lock_bytes.decode())
    readme_bytes = (module / project["readme"]).read_bytes()
    verified = validate_wheel(module, wheel, project)
    audited, audited_wheel_bytes = audit_forbiddenfruit(lock, forbiddenfruit_wheel, forbiddenfruit_sdist)
    command = [uv, "export", "--project", str(module), "--locked", "--offline", "--extra", "temporal",
               "--no-dev", "--no-emit-project", "--no-header", "--no-annotate"]
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=False)
    if result.returncode:
        raise ValueError("locked_offline_export_failed")
    validate_requirements(result.stdout, lock, project)
    requirements = add_audited_wheel_hash(result.stdout, audited)
    dockerfile = (module / "Dockerfile.task").read_bytes()
    if re.findall(r"^FROM\s+(\S+)", dockerfile.decode(), re.MULTILINE) != [BASE_IMAGE, BASE_IMAGE]:
        raise ValueError("dockerfile_base_mismatch")
    wheel_bytes = wheel.read_bytes()
    if (sha256(wheel_bytes) != verified["wheel_sha256"]
            or {name: sha256(value) for name, value in source_files(module).items()} != verified["source_files"]
            or (module / "pyproject.toml").read_bytes() != project_bytes
            or (module / "uv.lock").read_bytes() != lock_bytes
            or (module / project["readme"]).read_bytes() != readme_bytes):
        raise ValueError("build_inputs_changed")
    payloads = {"Dockerfile": dockerfile, "requirements.txt": requirements,
                "wheels/" + wheel.name: wheel_bytes,
                ".dockerignore": b"*\n!.dockerignore\n!Dockerfile\n!requirements.txt\n!manifest.json\n!wheels/\n!wheels/*.whl\n"}
    if audited:
        payloads["wheels/" + audited["filename"]] = audited_wheel_bytes
    manifest = {"schema_version": 1, "kind": "mini-loop-task-build-context", "base_image": BASE_IMAGE,
                "project": {"name": project["name"], "version": project["version"]},
                "inputs": {"pyproject.toml": sha256(project_bytes), "uv.lock": sha256(lock_bytes),
                           project["readme"]: sha256(readme_bytes),
                           "Dockerfile.task": sha256(dockerfile)},
                "files": {name: {"sha256": sha256(value), "bytes": len(value)} for name, value in payloads.items()},
                "wheel_validation": verified,
                "audited_dependency_wheels": [audited] if audited else [],
                "export_policy": "uv export --locked --offline --extra temporal --no-dev --no-emit-project --no-header --no-annotate",
                "excluded": ["environment files", "deployment secrets", "tests", "scripts", "tooling", "runtime state"],
                "runtime_install_policy": "locked hashes plus one audited forbiddenfruit wheel hash; binary wheels only; full default dependencies plus temporal"}
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(tempfile.mkdtemp(prefix=".task-bundle-", dir=output.parent))
    try:
        for name, value in payloads.items():
            path = temporary / name
            path.parent.mkdir(exist_ok=True)
            path.write_bytes(value)
        (temporary / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
        os.rename(temporary, output)
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)
    return manifest


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--wheel", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--module", default=MODULE, type=Path)
    parser.add_argument("--uv", default="uv")
    parser.add_argument("--forbiddenfruit-wheel", type=Path)
    parser.add_argument("--forbiddenfruit-sdist", type=Path)
    args = parser.parse_args(argv)
    try:
        result = build_bundle(args.module, args.wheel, args.output, args.uv,
                              forbiddenfruit_wheel=args.forbiddenfruit_wheel, forbiddenfruit_sdist=args.forbiddenfruit_sdist)
    except (ValueError, OSError, KeyError, tarfile.TarError, zipfile.BadZipFile, subprocess.TimeoutExpired) as error:
        parser.error(type(error).__name__ + ": " + str(error) if isinstance(error, ValueError) else "bundle_creation_failed")
    print(json.dumps({"status": "created", "output": str(args.output),
                      "manifest_sha256": sha256((args.output / "manifest.json").read_bytes()),
                      "files": sorted(result["files"])}, indent=2))


if __name__ == "__main__":
    main()
