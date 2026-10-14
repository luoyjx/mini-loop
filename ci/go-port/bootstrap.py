#!/usr/bin/env python3
"""Prepare public, pinned inputs for the Linux arm64 Go-port CI gates.

The network/Docker entry point runs only on a GitHub Actions Linux arm64 host.
--check-assets is a local, read-only data check. This does not run any gate,
upload images/caches, read application secrets, or install project dependencies.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import ssl
import subprocess
import time
from urllib.parse import quote, urlsplit
from urllib.request import HTTPSHandler, HTTPRedirectHandler, ProxyHandler, Request, build_opener
import uuid

ASSET_HASHES = {
    "linux-source-plan.json": "f17af01da903e988e4bba8e6b999be7d9e87dc8459780a5d54c336e9eeac1a54",
    "linux-source-capture.py": "08c6ece6a7aa909f28d036bcc15c7697ddb9bb6ea807f7d0c5a975dd4110e632",
    "linux-wheel-manifest.json": "9c7205001057c49ffca8bcf4053afe01d6d4f9b970d217fefccc3926979d66e9",
    "remote-go-image-resolution.json": "104ba2c09e218d92d5b44c40a781fc72adfa1e0e73d592bbf6969dffc3ecf2f2",
}
GO_MANIFEST = "sha256:53a9c8dafa550eb772ff742e0a8b80a5cfabd7fdb7635fe0e5e5fa413c85a848"
GO_IMAGE = "golang:1.23.3-bookworm@" + GO_MANIFEST
GO_CONFIG = "sha256:874a8b40ad6058f3f160636c61946394559b067562d7478cc3f9cc500794e9aa"
PYTHON_MANIFEST = "sha256:eaee5f73efa9ae962d2077756292bc4878c04fcbc13dc168bb00cc365f35647e"
PYTHON_IMAGE = "python:3.11.3-slim@" + PYTHON_MANIFEST
PYTHON_CONFIG = "sha256:8c1f8aceb3c61797f503382247331e926324957d770dceb460ab0daa8f177f98"
MODULE = "github.com/mattn/go-sqlite3"
MODULE_VERSION = "v1.14.52"
MEMORY = 3 * 1024 * 1024 * 1024
LABEL = "io.mini-loop.go-port.bootstrap"
EXPECTED_DOCKERFILE = (
    f"FROM {PYTHON_IMAGE} AS parser\nFROM {GO_IMAGE}\n"
    "COPY --from=parser /usr/local /usr/local\n"
    'RUN ldconfig && /usr/local/bin/python3.11 -I -S -B -c "import ast,base64,json,resource,sys; '
    "assert sys.implementation.name == 'cpython'; assert sys.version_info[:3] == (3,11,3); "
    "ast.parse('return 1')\" && /usr/bin/python3 -I -S -B -c \"import sys; "
    'assert sys.version_info[:3] == (3,11,2)"\n'
).encode("utf-8")


def require(condition: bool, message: str) -> None:
    if not condition:
        raise RuntimeError(message)


class StopRequested(BaseException):
    pass


def request_stop(signum, frame) -> None:
    raise StopRequested("bootstrap received signal " + str(signum))


def reference(path: Path) -> dict:
    require(path.is_file() and not path.is_symlink(), "missing or symlink input: " + str(path))
    with path.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    return {"path": str(path.resolve()), "bytes": path.stat().st_size, "sha256": digest}


def save(path: Path, value: dict) -> None:
    temporary = path.with_name(path.name + ".writing")
    temporary.write_text(json.dumps(value, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def check_assets(repo: Path) -> dict:
    assets = repo / "ci/go-port/assets"
    references = {}
    for name, expected in ASSET_HASHES.items():
        observed = reference(assets / name)
        require(observed["sha256"] == expected, "fixed asset differs: " + name)
        references[name] = observed
    require((assets / "Dockerfile").read_bytes() == EXPECTED_DOCKERFILE,
            "Dockerfile must retain the fixed original bytes except the public Go FROM reference")
    references["Dockerfile"] = reference(assets / "Dockerfile")
    resolution = json.loads((assets / "remote-go-image-resolution.json").read_text())
    require(resolution["platform_manifest_digest"] == GO_MANIFEST
            and resolution["config_digest"] == GO_CONFIG
            and resolution["manifest"]["config"]["digest"] == GO_CONFIG
            and resolution["registry_manifests_verified_by_sha256"] is True,
            "public Go manifest/config resolution differs")
    manifest = json.loads((assets / "linux-wheel-manifest.json").read_text())
    require(len(manifest) == 22 and len({row["name"] for row in manifest}) == 22,
            "existing Source wheel inventory differs")
    for row in manifest:
        require(re.fullmatch(r"[A-Za-z0-9_.-]+\.whl", row["name"]) is not None
                and re.fullmatch(r"[0-9a-f]{64}", row["sha256"]) is not None
                and isinstance(row["bytes"], int) and 0 < row["bytes"] <= 16 * 1024 * 1024,
                "invalid fixed wheel row")
    return {"assets": references, "wheels": manifest}


def permitted_url(url: str, host: str) -> bool:
    parsed = urlsplit(url)
    return (parsed.scheme == "https" and parsed.hostname == host
            and parsed.port in (None, 443) and parsed.username is None
            and parsed.password is None and not parsed.fragment)


class PinnedRedirect(HTTPRedirectHandler):
    def __init__(self, host: str):
        self.host = host

    def redirect_request(self, request, response, code, message, headers, newurl):
        require(permitted_url(newurl, self.host), "download redirect left the allowed HTTPS host")
        return super().redirect_request(request, response, code, message, headers, newurl)


def download(url: str, host: str, target: Path, maximum: int) -> dict:
    require(permitted_url(url, host), "download URL is not the allowed public HTTPS host")
    opener = build_opener(ProxyHandler({}), HTTPSHandler(context=ssl.create_default_context()), PinnedRedirect(host))
    request = Request(url, headers={"User-Agent": "mini-loop-fixed-go-port-inputs/1", "Accept-Encoding": "identity"})
    temporary = target.with_name(target.name + ".part")
    try:
        with opener.open(request, timeout=45) as response, temporary.open("xb") as output:
            require(response.status == 200 and permitted_url(response.geturl(), host), "unexpected public download response")
            require(response.headers.get("Content-Encoding", "identity") == "identity", "encoded download is not permitted")
            length = 0
            while chunk := response.read(65536):
                length += len(chunk)
                require(length <= maximum, "public download exceeded its fixed byte bound")
                output.write(chunk)
        require(not target.exists(), "refuse overwriting downloaded input")
        temporary.replace(target)
    finally:
        temporary.unlink(missing_ok=True)
    return {"url": url, "file": reference(target)}


def download_wheels(out: Path, manifest: list[dict]) -> list[dict]:
    metadata = out / "download-metadata"
    wheels = out / "assets/linux-wheels"
    metadata.mkdir()
    wheels.mkdir()
    records = []
    for row in manifest:
        distribution, version = row["name"].split("-")[:2]
        project = distribution.replace("_", "-")
        url = f"https://pypi.org/pypi/{quote(project, safe='')}/{quote(version, safe='')}/json"
        metadata_path = metadata / f"{project}-{version}.json"
        discovered = download(url, "pypi.org", metadata_path, 8 * 1024 * 1024)
        data = json.loads(metadata_path.read_text(encoding="utf-8"))
        require(data["info"]["version"] == version, "PyPI returned a different project version")
        normalized = lambda value: re.sub(r"[-_.]+", "-", value).lower()
        require(normalized(data["info"]["name"]) == normalized(project), "PyPI returned a different project")
        selected = [value for value in data["urls"] if value["filename"] == row["name"]
                    and value["digests"]["sha256"] == row["sha256"] and value["size"] == row["bytes"]
                    and value["packagetype"] == "bdist_wheel"]
        require(len(selected) == 1, "fixed wheel is absent or ambiguous in exact PyPI release: " + row["name"])
        observed = download(selected[0]["url"], "files.pythonhosted.org", wheels / row["name"], row["bytes"])
        require(observed["file"]["bytes"] == row["bytes"] and observed["file"]["sha256"] == row["sha256"],
                "downloaded wheel does not match the original Source manifest: " + row["name"])
        records.append({"name": row["name"], "project": project, "version": version,
                        "metadata": discovered, "wheel": observed})
    return records


class Commands:
    def __init__(self, out: Path, record: dict):
        self.out, self.record = out, record
        self.logs = out / "logs"
        self.logs.mkdir()
        for name in ("home", "tmp", "docker-config"):
            (out / name).mkdir()
        # No GitHub, application, SSH, proxy or inherited Docker credentials.
        self.env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "HOME": str(out / "home"),
                    "TMPDIR": str(out / "tmp"), "DOCKER_CONFIG": str(out / "docker-config"),
                    "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "GOMAXPROCS": "2",
                    "GOMEMLIMIT": "1GiB", "GOFLAGS": "-p=1", "GOGC": "75"}

    def run(self, label: str, argv: list[str], *, timeout: int = 900, check: bool = True) -> str:
        log = self.logs / f"{len(self.record['commands']):02}-{label}.log"
        row = {"name": label, "argv": argv, "timeout_seconds": timeout,
               "started_utc": datetime.now(timezone.utc).isoformat()}
        self.record["commands"].append(row)
        started = time.monotonic()
        try:
            with log.open("xb") as output:
                process = subprocess.Popen(argv, env=self.env, stdout=output, stderr=subprocess.STDOUT,
                                           start_new_session=True)
                try:
                    row["exit_code"] = process.wait(timeout=timeout)
                except BaseException:
                    if process.poll() is None:
                        os.killpg(process.pid, signal.SIGTERM)
                        try:
                            process.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            os.killpg(process.pid, signal.SIGKILL)
                            process.wait()
                    row["exit_code"] = process.returncode
                    raise
        finally:
            row["seconds"] = time.monotonic() - started
            row["finished_utc"] = datetime.now(timezone.utc).isoformat()
            if log.exists():
                row["output"] = reference(log)
            save(self.out / "bootstrap.json", self.record)
        require(not check or row["exit_code"] == 0, "bootstrap command failed: " + label)
        return log.read_text(encoding="utf-8", errors="replace")

    def container(self, label: str, image: str, command: list[str], *, network: str = "none",
                  mounts: tuple[tuple[Path, str, bool], ...] = (), environment: dict | None = None) -> str:
        token = uuid.uuid4().hex
        name = "mini-loop-ci-bootstrap-" + token
        argv = ["docker", "run", "--rm", "--name", name, "--label", LABEL + "=" + token,
                "--platform", "linux/arm64", "--pull", "never", "--read-only", "--network", network,
                "--cpus", "2", "--memory", str(MEMORY), "--memory-swap", str(MEMORY), "--pids-limit", "256",
                "--user", f"{os.getuid()}:{os.getgid()}", "--tmpfs", "/tmp:rw,nosuid,nodev,size=268435456"]
        child_env = {"HOME": "/tmp/home", "PATH": "/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                     "LANG": "C.UTF-8", "PYTHONDONTWRITEBYTECODE": "1", "GOMAXPROCS": "2",
                     "GOMEMLIMIT": "1GiB", "GOGC": "75", "GOFLAGS": "-p=1", "GOTOOLCHAIN": "local"}
        child_env.update(environment or {})
        for key, value in child_env.items():
            argv += ["-e", key + "=" + value]
        for source, destination, readonly in mounts:
            argv += ["--mount", "type=bind,src=" + str(source) + ",dst=" + destination + (",readonly" if readonly else "")]
        argv += [image, "nice", "-n", "10", *command]
        try:
            return self.run(label, argv)
        finally:
            previous = {sig: signal.getsignal(sig) for sig in (signal.SIGINT, signal.SIGTERM)}
            for sig in previous:
                signal.signal(sig, signal.SIG_IGN)
            try:
                observed = self.run(label + "-owned-inspect", ["docker", "container", "inspect", name], timeout=30, check=False)
                if self.record["commands"][-1]["exit_code"] == 0:
                    rows = json.loads(observed)
                    require(len(rows) == 1 and rows[0]["Config"]["Labels"].get(LABEL) == token,
                            "refuse cleanup of a container without the owned bootstrap label")
                    self.run(label + "-owned-cleanup", ["docker", "container", "rm", "--force", rows[0]["Id"]], timeout=30)
                empty = self.run(label + "-empty-check", ["docker", "container", "ls", "-aq", "--filter", "label=" + LABEL + "=" + token], timeout=30)
                require(not empty.strip(), "owned bootstrap container remains")
            finally:
                for sig, handler in previous.items():
                    signal.signal(sig, handler)


def inspect_image(commands: Commands, label: str, image: str, config: str | None = None,
                  manifest: str | None = None, repository: str | None = None) -> dict:
    rows = json.loads(commands.run(label, ["docker", "image", "inspect", image], timeout=30))
    require(len(rows) == 1 and rows[0]["Architecture"] == "arm64" and rows[0]["Os"] == "linux", "image platform differs")
    observed = rows[0]
    if config:
        require(observed["Id"] == config, "pulled official image config differs from the fixed original")
    if manifest:
        require(any(value.split("@")[-1] == manifest and value.split("@")[0] in
                    {repository, "library/" + repository, "docker.io/library/" + repository}
                    for value in observed.get("RepoDigests", [])), "pulled official RepoDigest differs")
    return {key: observed.get(key) for key in ("Id", "Architecture", "Os", "RepoDigests", "Created")}


def python_probe(commands: Commands, label: str, image: str, executable: str, version: str) -> dict:
    code = ("import ast,base64,hashlib,json,os,resource,sys; "
            "path=os.path.realpath(sys.executable); "
            "row={'implementation':sys.implementation.name,'version':'.'.join(map(str,sys.version_info[:3])),"
            "'path':path,'executable_sha256':hashlib.sha256(open(path,'rb').read()).hexdigest(),"
            "'isolated':sys.flags.isolated,'no_site':sys.flags.no_site,'dont_write_bytecode':sys.dont_write_bytecode}; "
            "ast.parse('return 1'); print(json.dumps(row,sort_keys=True))")
    value = json.loads(commands.container(label, image, [executable, "-I", "-S", "-B", "-c", code]))
    require(value["implementation"] == "cpython" and value["version"] == version
            and value["isolated"] == value["no_site"] == value["dont_write_bytecode"] == 1,
            "actual Python runtime probe differs: " + label)
    return value


def module_inputs(repo: Path, out: Path) -> dict:
    folder = out / "module-inputs"
    folder.mkdir()
    module_text = (repo / "go/go.mod").read_text()
    lines = [line.strip() for line in module_text.splitlines() if line.strip()]
    require(lines == ["module github.com/luoyjx/mini-loop/go", "go 1.23", f"require {MODULE} {MODULE_VERSION}"],
            "review the changed Go dependency inventory before CI preparation")
    sums = [line.split() for line in (repo / "go/go.sum").read_text().splitlines() if line.strip()]
    require(len(sums) == 2 and {(row[0], row[1]) for row in sums} ==
            {(MODULE, MODULE_VERSION), (MODULE, MODULE_VERSION + "/go.mod")}
            and all(len(row) == 3 and row[2].startswith("h1:") for row in sums), "unexpected Go sum inventory")
    for name in ("go.mod", "go.sum"):
        shutil.copyfile(repo / "go" / name, folder / name)
    return {name: reference(folder / name) for name in ("go.mod", "go.sum")}


def module_manifest(modules: Path) -> dict:
    entries = {}
    for path in sorted(modules.rglob("*")):
        require(not path.is_symlink(), "unexpected symlink in fresh public module cache")
        if path.is_file():
            observed = reference(path)
            entries[str(path.relative_to(modules))] = {"bytes": observed["bytes"], "sha256": observed["sha256"]}
        else:
            require(path.is_dir(), "nonregular public module cache member")
    require(entries, "public module cache is empty")
    return entries


def bootstrap(repo: Path, out: Path, checked: dict) -> int:
    require(os.environ.get("GITHUB_ACTIONS") == "true" and platform.system() == "Linux"
            and platform.machine().lower() in ("aarch64", "arm64"),
            "network/Docker preparation requires GITHUB_ACTIONS=true on Linux arm64")
    require(out != repo and not out.exists(), "bootstrap output must be a new directory")
    os.umask(0o077)
    out.mkdir(parents=True)
    record = {"schema_version": 1, "status": "preparing", "started_utc": datetime.now(timezone.utc).isoformat(),
              "repo_root": str(repo), "out": str(out), "platform": {"system": platform.system(), "machine": platform.machine()},
              "commands": [], "assets": {}, "downloads": [], "project_dependencies_added": False,
              "private_cache_or_image_uploaded": False, "gates_executed": False,
              "scope": "Remote public input preparation only; no full-port acceptance or gate pass is inferred."}
    save(out / "bootstrap.json", record)
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, request_stop)
    try:
        assets = out / "assets"
        assets.mkdir()
        for name in checked["assets"]:
            shutil.copyfile(repo / "ci/go-port/assets" / name, assets / name)
            record["assets"][name] = reference(assets / name)
        record["downloads"] = download_wheels(out, checked["wheels"])
        commands = Commands(out, record)
        base = {}
        for label, image, config, manifest in (("go", GO_IMAGE, GO_CONFIG, GO_MANIFEST),
                                                ("python", PYTHON_IMAGE, PYTHON_CONFIG, PYTHON_MANIFEST)):
            commands.run("pull-" + label, ["docker", "pull", "--platform", "linux/arm64", image])
            base[label] = {"reference": image, "config_digest": config,
                           "inspection": inspect_image(commands, "inspect-" + label, image, config, manifest, label if label != "go" else "golang")}
        base["go"]["source_probe"] = python_probe(commands, "base-source-probe", GO_IMAGE, "/usr/bin/python3", "3.11.2")
        base["python"]["native_probe"] = python_probe(commands, "base-native-probe", PYTHON_IMAGE, "/usr/local/bin/python3.11", "3.11.3")
        context = out / "build-context"
        context.mkdir()
        shutil.copyfile(assets / "Dockerfile", context / "Dockerfile")
        iid = out / "derived-image.id"
        commands.run("build-derived", ["docker", "build", "--platform", "linux/arm64", "--pull=false", "--no-cache",
                                      "--network=none", "--iidfile", str(iid), str(context)])
        image = iid.read_text().strip()
        require(re.fullmatch(r"sha256:[0-9a-f]{64}", image) is not None, "invalid built image ID")
        inspection = inspect_image(commands, "inspect-derived", image)
        probes = {"source": python_probe(commands, "derived-source-probe", image, "/usr/bin/python3", "3.11.2"),
                  "native": python_probe(commands, "derived-native-probe", image, "/usr/local/bin/python3.11", "3.11.3")}
        go_version = commands.container("derived-go-probe", image, ["go", "version"]).strip()
        require(go_version == "go version go1.23.3 linux/arm64", "actual Go toolchain probe differs")
        probes["go"] = go_version
        image_record = {"schema_version": 1, "image_id": image, "architecture": "arm64", "os": "linux",
                        "source_runtime": "/usr/bin/python3 CPython3.11.2", "native_runtime": "/usr/local/bin/python3.11 CPython3.11.3",
                        "base_images": base, "inspection": inspection, "dockerfile": reference(context / "Dockerfile"),
                        "probes": probes, "build_network": "none", "registry_upload": False,
                        "scope": "New remote derived image ID; Source and native gates must both bind this image, not a historical local ID."}
        save(out / "linux-image.json", image_record)
        record["image"] = reference(out / "linux-image.json")
        record["module_inputs"] = module_inputs(repo, out)
        modules = out / "modules"
        modules.mkdir()
        script = "mkdir -p /tmp/home && cd /inputs && go mod download && go mod verify"
        commands.container("existing-module-download-verify", image, ["sh", "-eu", "-c", script], network="default",
                           mounts=((out / "module-inputs", "/inputs", True), (modules, "/gomod", False)),
                           environment={"GOMODCACHE": "/gomod", "GOPATH": "/tmp/gopath", "GOCACHE": "/tmp/gocache",
                                        "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org",
                                        "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": ""})
        record["modules"] = {"path": str(modules), "approved_module": MODULE, "version": MODULE_VERSION,
                             "source": "fresh public proxy download verified by committed go.sum; no uploaded local cache",
                             "gate_mount_policy": "read-only with GOPROXY=off/GOSUMDB=off and --network=none",
                             "manifest": module_manifest(modules)}
        for name, before in record["module_inputs"].items():
            require(reference(out / "module-inputs" / name) == before, "module preparation mutated fixed input")
        require(check_assets(repo) == checked, "tracked assets changed during bootstrap")
        record["status"] = "prepared_pinned_public_inputs_no_gates_executed"
        return 0
    except BaseException as error:
        record.update(status="failed", error_type=type(error).__name__, error=str(error))
        return 1
    finally:
        record["completed_utc"] = datetime.now(timezone.utc).isoformat()
        save(out / "bootstrap.json", record)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--out", type=Path, help="new output directory for public inputs and actual preparation records")
    parser.add_argument("--check-assets", action="store_true", help="read-only local fixed-input check; no downloads, Docker or output writes")
    args = parser.parse_args()
    repo = args.repo_root.resolve(strict=True)
    checked = check_assets(repo)
    if args.check_assets:
        print(json.dumps({"status": "fixed_assets_match", "assets": checked["assets"], "existing_wheels": 22}, sort_keys=True))
        return 0
    if args.out is None:
        parser.error("--out is required for remote preparation")
    return bootstrap(repo, args.out.resolve(), checked)


if __name__ == "__main__":
    raise SystemExit(main())
