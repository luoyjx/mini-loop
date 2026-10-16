"""Bind an explicitly selected local image to its audited build and source bytes."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re

from tooling.build_task_bundle import source_files
from tooling.paths import MODULE
from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.container_checks import PYTHON
from tooling.acceptance.support.process import HarnessFailure


MAX_MANIFEST_BYTES = 1024 * 1024


def validate_image_id(value: str) -> str:
    if re.fullmatch(r"sha256:[0-9a-f]{64}", value) is None:
        raise HarnessFailure("An immutable local image ID sha256:<64 lowercase hex digits> is required")
    return value


def validate_manifest(path: Path, *, module: Path = MODULE) -> dict:
    """Compare the complete package file set, avoiding paths supplied by a manifest."""
    if not path.is_file() or not 0 < path.stat().st_size <= MAX_MANIFEST_BYTES:
        raise HarnessFailure("A bounded existing build manifest is required")
    with path.open("rb") as stream:
        content = stream.read(MAX_MANIFEST_BYTES + 1)
    if len(content) > MAX_MANIFEST_BYTES:
        raise HarnessFailure("The selected build manifest exceeds its byte budget")
    manifest = json.loads(content)
    if manifest.get("schema_version") != 1 or manifest.get("kind") != "mini-loop-task-build-context":
        raise HarnessFailure("The selected file is not a Task image build manifest")
    current = {name: hashlib.sha256(value).hexdigest() for name, value in source_files(module).items()}
    if current != manifest["wheel_validation"]["source_files"]:
        raise HarnessFailure("Runtime source no longer matches the selected image build manifest")
    for name in ("pyproject.toml", "uv.lock", "Dockerfile.task"):
        if hashlib.sha256((module / name).read_bytes()).hexdigest() != manifest["inputs"][name]:
            raise HarnessFailure("Runtime dependency or image inputs no longer match the selected build manifest")
    return {"manifest_sha256": hashlib.sha256(content).hexdigest(),
            "runtime_source_sha256": {f"{module.name}/src/{name}": digest for name, digest in current.items()},
            "lock_sha256": manifest["inputs"]["uv.lock"],
            "build_readme_sha256": manifest["inputs"]["README.md"],
            "current_documentation_is_not_image_build_metadata": True}


IMAGE_MANIFEST_PROBE = """
import hashlib, importlib.metadata, json, pathlib
raw = pathlib.Path('/opt/task/build-manifest.json').read_bytes()
manifest = json.loads(raw)
distribution = importlib.metadata.distribution('mini-loop-deepagents')
actual = {str(name): hashlib.sha256(distribution.locate_file(name).read_bytes()).hexdigest()
          for name in distribution.files
          if str(name).startswith('mini_loop_deepagents/')
          and '__pycache__' not in str(name).split('/') and name.suffix not in ('.pyc', '.pyo')}
expected = manifest['wheel_validation']['source_files']
assert actual == expected, 'installed_source_does_not_match_manifest'
print(json.dumps({'manifest_sha256': hashlib.sha256(raw).hexdigest(),
                  'installed_package_files': len(actual), 'installed_source_matches_manifest': True}))
"""


async def verify_image_manifest(containers, processes, expected_sha256: str) -> dict:
    """Read the selected image using an owned, bounded, secret-free probe container."""
    probe = containers.create("tc-" + containers.run_id[:12] + "-manifest",
                              ["-c", IMAGE_MANIFEST_PROBE], {}, entrypoint=[PYTHON], transient=True)

    async def exited():
        return containers.owned(probe)["State"]

    state = await SUPPORT.eventually(processes, exited, lambda value: not value["Running"], timeout=7)
    if state["ExitCode"] != 0 or state.get("OOMKilled"):
        raise HarnessFailure("The selected image did not verify its installed package against its embedded manifest")
    result = json.loads(containers.logs(probe))
    if (result.get("manifest_sha256") != expected_sha256
            or result.get("installed_source_matches_manifest") is not True):
        raise HarnessFailure("The selected image does not contain the selected build manifest")
    return {"container_id": probe["id"], "exit_code": state["ExitCode"], **result}
