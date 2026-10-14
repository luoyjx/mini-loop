#!/usr/bin/env python3
"""Verify complete Linux CI evidence without running tests, Docker, or downloads.

The CI commit is bound separately from the exact 606630f runtime byte baseline.
Only a new CI receipt is published. This verifier never updates the 344-row
ledger or declares full port acceptance. --check revalidates extracted evidence;
explicit recorded-root mappings relocate references without rewriting raw JSON.
"""
from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile
import uuid
from urllib.parse import urlsplit

BASELINE = "606630f5c894b03cfccaafa1d2158e3bb07863cf"
HOST = "docs/audits/go-native-idna-progress-2026-10-15.json"
HOST_SHA = "f19801fac73814395621a7b1995bfe4e38ed04cd32357b3b04ec035742562c13"
G5 = "docs/audits/go-persistence-required-recipe-ledger-2026-10-14.json"
G5_SHA = "df1fd9ca53f60e9a6fffa92a69d2b360ad5987b020458d7a93f9c9eb0a120312"
MODULE = "github.com/luoyjx/mini-loop/go"
PACKAGE_SUFFIXES = """agent astcontext audit background benchmark cmd/miniloop cmd/miniloop-audit cmd/traceview
config cron decisions diagnostics dotenv durable efficiency httpapi identity improvement internal/commandenv
internal/fnmatch internal/jsonsyntax internal/jsonvalue internal/punycode internal/pytext launcher mcp memory
problems protocol provider reload runmeta runtimeconfig sandbox secrets selfaudit selfimprove sessionquery shell
skills spill sqlitestate tasks teams tools/contractaudit tools/typeaudit traceview trajectory userresources
verifiedloop workflows workspace worktrees""".split()
PACKAGES = {MODULE + "/" + name for name in PACKAGE_SUFFIXES}
GROUPS = ("benchmark", "httpapi", "httpapi-efficiency", "userresources", "workspace", "worktrees")
GROUP_COUNTS = (8, 5, 2, 4, 38, 9)
STAGES = {mode: "linux-ci-" + mode for mode in ("source", "full", "vet", "race")}
ASSETS = {
    "linux-source-plan.json": "f17af01da903e988e4bba8e6b999be7d9e87dc8459780a5d54c336e9eeac1a54",
    "linux-source-capture.py": "08c6ece6a7aa909f28d036bcc15c7697ddb9bb6ea807f7d0c5a975dd4110e632",
    "linux-wheel-manifest.json": "9c7205001057c49ffca8bcf4053afe01d6d4f9b970d217fefccc3926979d66e9",
}
PROFILE = {"cpu_cgroup_cores": 2, "memory_cgroup_bytes": 3221225472, "memory_and_swap_bytes": 3221225472,
           "gomaxprocs": 2, "gogc": 75, "gomemlimit": "1GiB soft target", "go_package_parallel": 1,
           "test_parallel": 2, "nice": 10, "pids": 256, "preflight_samples": 5, "automatic_retries": False}
PRESSURE = {"source": "Linux /proc/meminfo and /proc/pressure/memory", "minimum_available_bytes": 6442450944,
            "minimum_available_fraction": 0.20, "maximum_full_avg10": 0.0, "maximum_some_avg10": 5.0,
            "read_failure": "stop_without_launch_or_stop_owned_container"}
CORE = {
    "protocol": "TestContentBoundary TestToolInputsRejectUnknownMissingAndInvalidValues TestToolInputCannotBeDecodedWithoutItsName TestTypedInputsTrackPythonSchema TestSDKUnicodeObservationPreservesNamedFieldsAndStrictWire".split(),
    "agent": "TestFakeTurnAndSessionIsolation TestManagedCancelTargetsActiveTurnAndQueueRemainsUsable TestCoreQueuedTurnReportsAndCancelsBeforeMutation TestActualLoopNudgesAndHaltsMatchPython TestCatalogCopiesCapabilitiesAndRejectsDuplicates TestPermissionModesAndImmutableDenyList TestGateOrdersRewriteGuardPermissionExecuteAfterObserver TestGuardDenialCannotBeLaunderedByPostHook TestPermissionPublicationCancellationStopsLaterApprovalAndHandler TestSDKSurrogateBashReachesSafeFilesystemBoundaryWithoutJournal TestActualSourceTeamlessHTTPThroughOwnedHandler TestRecoveryMatchesActualPython TestDroppedStreamRegeneratesWithoutSplicingAndReleasesModelPermit TestCacheWireMatchesPythonProjections TestActualProviderRequestsCarryCacheButHistoryDoesNot TestCachePolicyAppliesToSummaryRequest TestExplicitFeatureCatalogueMatchesSourceDefinitions".split(),
    "provider": "TestDirectHTTPMatchesActualPythonSDK TestStreamingMatchesActualSDKAndPythonTransport TestStreamingDoesNotRetryDroppedBodyOrCallback TestRealStreamingToolRoundPreservesSignedHistoryUsageAndPhases TestActualStreamingCancellationAndTimeoutCloseResponse TestWholeUsageActualHTTPManagedModelEnd TestEscapedSurrogateIngressAndNextRequestMatchActualSDK".split(),
    "launcher": ["TestDefaultTrajectoriesAreOwnedConfiguredAndRetained"],
    "httpapi": ["TestTrajectoryOwnershipPrecedesBodyAndJSONBound"],
}
PARSER_TOP = {
    "diagnostics": "TestCPythonParserActualSource62Profile TestCPythonParserConcurrentCallsAndInputBoundaries TestCPythonParserIsolationVersionAndNoExecution TestCPythonParserCancellationJoinsProcessAndBoundsConcurrentStarts TestCPythonParserOutputBudgetFailsWithoutCleanResult TestPythonSyntaxNativeScannerActualSourceRecipes TestPythonSyntaxInvalidResultsFaultsAndCancellation".split(),
    "agent": "TestManagerFullFeaturesUsesActualDefaultCPython TestManagerFullFeaturesMissingParserFailsBeforeWorkspaceEffects TestManagerFullFeaturesWrongPythonVersionFailsBeforeWorkspaceEffects TestExplicitFeatureCatalogueMatchesSourceDefinitions".split(),
    "launcher": "TestDefaultFullCPythonDiagnosticsThroughActualHTTP TestDefaultFullWrongPythonVersionFailsBeforeResources TestFullFeatureInspectionResolvesExplicitBackendWithoutEffects TestFullFeatureLauncherHTTPPublishesSourceCatalogueAndRepositoryBinding".split(),
    "cmd/miniloop": ["TestFullFeatureCLIStartsWithDefaultCPython"],
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def read(path):
    with Path(path).open() as stream:
        return json.load(stream)


def relative(name):
    path = PurePosixPath(name)
    require(name and not path.is_absolute() and ".." not in path.parts, "unsafe relative evidence path")
    return path


def stream_json(path):
    with Path(path).open() as stream:
        for number, line in enumerate(stream, 1):
            if line.strip():
                value = json.loads(line)
                require(isinstance(value, dict), "JSON event is not an object")
                yield number, value


class Evidence:
    def __init__(self, args):
        self.repo = args.repo_root.resolve(strict=True)
        self.out = args.out.resolve(strict=True)
        self.recorded_repo = args.recorded_repo_root or self.repo
        self.recorded_out = args.recorded_out or self.out
        require(self.recorded_repo.is_absolute() and self.recorded_out.is_absolute(), "recorded roots must be absolute")
        self.revision = args.revision
        self.artifact_check = args.check
        require(re.fullmatch(r"[0-9a-f]{40}", self.revision), "exact CI commit required")

    def local(self, name):
        path = Path(name)
        if not path.is_absolute():
            path = self.repo.joinpath(relative(str(path)))
        elif path.is_relative_to(self.recorded_out):
            path = self.out / path.relative_to(self.recorded_out)
        elif path.is_relative_to(self.recorded_repo):
            path = self.repo / path.relative_to(self.recorded_repo)
        else:
            raise ValueError("absolute reference requires an explicit recorded-root mapping: " + str(path))
        require(not path.is_symlink() and path.resolve(strict=True) == path, "noncanonical/symlink evidence path")
        require(path.is_relative_to(self.repo) or path.is_relative_to(self.out), "evidence escapes mapped roots")
        return path

    def ref(self, path):
        path = Path(path).resolve(strict=True)
        if path.is_relative_to(self.out):
            recorded = self.recorded_out / path.relative_to(self.out)
        else:
            require(path.is_relative_to(self.repo), "receipt reference escapes evidence roots")
            recorded = Path(str(path.relative_to(self.repo)))
        return {"path": str(recorded), "bytes": path.stat().st_size, "sha256": sha(path)}

    def check_ref(self, reference):
        path = self.local(reference["path"])
        require(path.is_file() and reference["bytes"] == path.stat().st_size and reference["sha256"] == sha(path), "referenced bytes changed")
        return path

    def hashes(self, base, manifest):
        for name, expected in manifest.items():
            path = base / relative(name)
            require(not path.is_symlink() and path.resolve(strict=True).is_relative_to(base.resolve()), "manifest path escapes root")
            require(sha(path) == expected, "manifest/log hash differs: " + str(path))

    def git(self, *argv):
        return subprocess.check_output(["git", "-C", str(self.repo), *argv]).decode().strip()

    def committed_file(self, name):
        path = self.repo / relative(name)
        process = subprocess.Popen(["git", "-C", str(self.repo), "show", self.revision + ":" + name], stdout=subprocess.PIPE)
        try:
            digest = hashlib.file_digest(process.stdout, "sha256").hexdigest()
            require(process.wait() == 0 and digest == sha(path), "CI implementation differs from the workflow commit: " + name)
        finally:
            process.stdout.close()
            if process.poll() is None:
                process.terminate()
                process.wait()
        return self.ref(path)


def runtime_inputs(api):
    require(api.git("rev-parse", "HEAD") == api.revision, "checkout is not the execution revision")
    host_path = api.repo / HOST
    require(sha(host_path) == HOST_SHA, "fixed public host manifest changed")
    host = read(host_path)
    require(host["runtime_commit"] == BASELINE and host["passed_current_host_gates"] == 16, "wrong original runtime baseline")
    manifest = host["runtime_inputs"]["manifest"]
    require(len(manifest) == 1864, "runtime input count changed")
    raw = subprocess.check_output(["git", "-C", str(api.repo), "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "go", "python", "AGENTS.md", "pytest.ini"])
    require({name.decode() for name in raw.split(b"\0") if name} == set(manifest), "runtime member set differs from606")
    api.hashes(api.repo, manifest)
    require(not api.git("status", "--porcelain", "--", "go", "python", "AGENTS.md", "pytest.ini"), "dirty CI runtime inputs")
    tools = [api.committed_file(name) for name in ("ci/go-port/verify.py", "ci/go-port/linux_runner.py", "ci/go-port/bootstrap.py", "ci/go-port/host_pressure.py")]
    require(os.environ.get("GITHUB_SHA", api.revision) == api.revision, "workflow/Git revision mismatch")
    return manifest, {"execution_revision": api.revision, "runtime_code_baseline": BASELINE,
                      "baseline_public_manifest": api.ref(host_path), "runtime_inputs": len(manifest),
                      "runtime_manifest_sha256": hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest(),
                      "same_runtime_bytes_as_baseline": True, "ci_implementation": tools}


def required_semantics(api, manifest):
    def fixture(name):
        require(manifest[name] == sha(api.repo / name), "semantic fixture differs from606")
        return read(api.repo / name)
    parser = fixture("go/testdata/python-syntax-parser.json")
    idna = fixture("go/testdata/python-provider-idna.json")
    puny = fixture("go/testdata/python-punycode.json")
    nfc = fixture("go/testdata/python-unicode-normalization.json")
    require(parser["implementation"] == "cpython" and parser["version"] == [3, 11, 3]
            and parser["operation"] == "ast.parse(source)" and len(parser["cases"]) == 62
            and Counter(row["kind"] for row in parser["cases"]) == {"clean": 24, "syntax": 36, "error": 2}, "parser62 profile differs")
    expected = {"python_implementation": "CPython", "python_version": "3.11.3", "unicode_version": "14.0.0",
                "sdk_version": "0.107.1", "httpx_version": "0.28.1", "idna_version": "3.18", "idna_table_unicode_version": "17.0.0"}
    require(all(idna.get(key) == value for key, value in expected.items()) and len(idna["cases"]) == 84
            and Counter(row["stage"] for row in idna["cases"]) == {"complete": 48, "constructor": 34, "request": 2}, "fixed IDNA84 profile differs")
    require(puny["reference"]["version"] == "3.11.3" and puny["generator_sha256"] == manifest["python/tools/export_go_punycode.py"]
            and len(puny["encodings"]) == 135 and len(puny["decodings"]) == 215, "raw Punycode135/215 profile differs")
    require(nfc["python_version"] == "3.11.3" and nfc["unicode_version"] == "14.0.0"
            and nfc["exporter_sha256"] == manifest["python/tools/export_go_normalization.py"]
            and len(nfc["normalization_cases"]) == 6320 and len(nfc["bidi_cases"]) == 2614 and len(nfc["invalid_utf8_cases"]) == 8, "NFC/Bidi profile differs")
    identities = {(MODULE + "/" + package, test) for package, names in CORE.items() for test in names}
    require(len(identities) == 31, "core31 inventory changed")
    for package, names in PARSER_TOP.items():
        identities.update((MODULE + "/" + package, test) for test in names)
    identities.update((MODULE + "/diagnostics", "TestCPythonParserActualSource62Profile/" + row["name"]) for row in parser["cases"])
    identities.add((MODULE + "/provider", "TestRequiredIDNAActualSourceDirectAndStream"))
    identities.update((MODULE + "/provider", "TestRequiredIDNAActualSourceDirectAndStream/" + row["mode"] + "/" + row["name"]) for row in idna["cases"])
    for key, direction in (("encodings", "encode"), ("decodings", "decode")):
        identities.update((MODULE + "/internal/punycode", "TestCPython3113DifferentialEncodeAndDecode/" + direction + "/" + row["name"]) for row in puny[key])
    for test in ("TestRFC3492RawPayloads", "TestScalarAndWorkBounds", "TestCPython3113DifferentialEncodeAndDecode"):
        identities.add((MODULE + "/internal/punycode", test))
    for test in ("TestNFC14MatchesActualCPythonNormalization", "TestNFC14RejectsInvalidUTF8WithoutReplacingIt", "TestBidiClass14MatchesActualCPythonProperties"):
        identities.add((MODULE + "/internal/pytext", test))
    identities.update((MODULE + "/internal/pytext", "TestNFC14MatchesActualCPythonNormalization/" + row["name"]) for row in nfc["normalization_cases"])
    identities.update((MODULE + "/internal/pytext", "TestNFC14RejectsInvalidUTF8WithoutReplacingIt/" + row["name"]) for row in nfc["invalid_utf8_cases"])
    ledger_path = api.repo / G5
    require(sha(ledger_path) == G5_SHA, "public immutable G5 recipe inventory changed")
    ledger = read(ledger_path)
    persistence = set()
    for group in ledger["groups"]:
        for test in group["native_tests"]:
            original = test["host_execution"]["go-full-corrected"]
            for row in [original["top_level"], *original["subtests"]]:
                persistence.add((row["event"]["Package"], row["event"]["Test"]))
    require(len(ledger["groups"]) == 7 and sum("/" not in test for _, test in persistence) == 25
            and sum("/" in test for _, test in persistence) == 49, "G5 original25/49 inventory differs")
    identities.update(persistence)
    return identities, {"parser": {"cases": 62, "clean": 24, "syntax": 36, "fault": 2},
                        "idna": {"cases": 84, "complete": 48, "constructor_refusal": 34, "request_refusal": 2},
                        "nfc_cases": 6320, "bidi_assertions_inside_top_test": 2614, "invalid_utf8_cases": 8,
                        "punycode_encodings": 135, "punycode_decodings": 215, "original_core_top_tests": 31,
                        "g5_top_tests": 25, "g5_subtests": 49, "g5_inventory_source": api.ref(ledger_path),
                        "required_named_identities": len(identities)}


def pressure_evidence(api, folder, record):
    require(record["allowed_host_pressure"] == ["normal"] and record["host_pressure_policy"] == PRESSURE, "Linux PSI policy differs")
    pressure_path = folder / "host-pressure-readings.jsonl"
    require(api.local(record["host_pressure_readings"]) == pressure_path, "PSI stage log reference differs")
    phases = []
    for _, row in stream_json(pressure_path):
        require(row["stage"] == folder.name and row["policy"] == PRESSURE and row["status"] == "normal" and row["reasons"] == [], "non-normal PSI observation")
        memory = {}
        for line in row["meminfo_raw"].splitlines():
            if line.startswith(("MemTotal:", "MemAvailable:")):
                key, amount, unit = line.split()
                require(key not in memory and unit == "kB" and amount.isdecimal(), "invalid raw meminfo")
                memory[key] = int(amount) * 1024
        total, available = memory["MemTotal:"], memory["MemAvailable:"]
        threshold = max(6442450944, (total + 4) // 5)
        require(0 < total and threshold <= available <= total and row["mem_total_bytes"] == total
                and row["mem_available_bytes"] == available and row["required_available_bytes"] == threshold, "MemAvailable policy/raw mismatch")
        psi = {}
        for line in row["memory_psi_raw"].splitlines():
            parts = line.split()
            require(parts and parts[0] in ("some", "full") and parts[0] not in psi, "invalid raw PSI row")
            values = dict(part.split("=", 1) for part in parts[1:])
            require(len(parts) == 5 and set(values) == {"avg10", "avg60", "avg300", "total"} and values["total"].isdecimal(), "incomplete raw PSI")
            averages = {key: Decimal(values[key]) for key in ("avg10", "avg60", "avg300")}
            require(all(value.is_finite() and 0 <= value <= 100 for value in averages.values()), "nonfinite/out-of-range PSI")
            require(all(Decimal(str(row["memory_psi"][parts[0]][key])) == value for key, value in averages.items())
                    and row["memory_psi"][parts[0]]["total"] == int(values["total"]), "recorded PSI/raw mismatch")
            psi[parts[0]] = averages
        require(set(psi) == {"some", "full"} and psi["full"]["avg10"] == 0 and psi["some"]["avg10"] <= 5, "PSI memory stall exceeds policy")
        phases.append(row["phase"])
    memory_phases = []
    for _, row in stream_json(folder / "memory.jsonl"):
        require(row["host_pressure"] == "normal", "stage contains warning/unavailable memory sample")
        memory_phases.append(row.get("phase", "running"))
    require(phases == memory_phases and Counter(phases)["preflight"] == 5 and Counter(phases)["immediately_before_start"] == 1
            and Counter(phases)["running"] > 0 and set(phases) == {"preflight", "immediately_before_start", "running"}, "PSI sampling/phase binding incomplete")
    return {"policy": PRESSURE, "raw": api.ref(pressure_path), "samples": dict(Counter(phases)), "all_recorded_samples_normal": True}


def unrestricted(argv, mode):
    expected = (["nice", "-n", "10", "go", "vet", "-p=1", "./..."] if mode == "vet" else
                ["nice", "-n", "10", "go", "test", "-json", "-count=1", "-parallel=2", "-p=1", "-timeout=30m"]
                + (["-race"] if mode == "race" else []) + ["./..."])
    require(argv == expected, "complete ordinary/vet/race command changed")


def bootstrap_evidence(api):
    bootstrap_path, image_path = api.out / "bootstrap.json", api.out / "linux-image.json"
    boot, image = read(bootstrap_path), read(image_path)
    require(boot["status"] == "prepared_pinned_public_inputs_no_gates_executed" and boot["completed_utc"]
            and api.local(boot["repo_root"]) == api.repo and api.local(boot["out"]) == api.out
            and boot["project_dependencies_added"] is False and boot["private_cache_or_image_uploaded"] is False
            and boot["gates_executed"] is False and boot["platform"]["system"] == "Linux"
            and boot["platform"]["machine"].lower() in ("aarch64", "arm64"), "public bootstrap incomplete or wrong platform")
    require(api.check_ref(boot["image"]) == image_path and image["schema_version"] == 1
            and image["architecture"] == "arm64" and image["os"] == "linux"
            and image["source_runtime"] == "/usr/bin/python3 CPython3.11.2"
            and image["native_runtime"] == "/usr/local/bin/python3.11 CPython3.11.3"
            and image["build_network"] == "none" and image["registry_upload"] is False
            and re.fullmatch(r"sha256:[0-9a-f]{64}", image["image_id"]), "derived image identity differs")
    pins = {
        "go": ("golang:1.23.3-bookworm@sha256:53a9c8dafa550eb772ff742e0a8b80a5cfabd7fdb7635fe0e5e5fa413c85a848",
               "sha256:874a8b40ad6058f3f160636c61946394559b067562d7478cc3f9cc500794e9aa"),
        "python": ("python:3.11.3-slim@sha256:eaee5f73efa9ae962d2077756292bc4878c04fcbc13dc168bb00cc365f35647e",
                   "sha256:8c1f8aceb3c61797f503382247331e926324957d770dceb460ab0daa8f177f98"),
    }
    for name, (reference, config) in pins.items():
        base = image["base_images"][name]
        require(base["reference"] == reference and base["config_digest"] == base["inspection"]["Id"] == config
                and base["inspection"]["Architecture"] == "arm64" and base["inspection"]["Os"] == "linux"
                and any(value.split("@")[-1] == reference.split("@")[1] for value in base["inspection"]["RepoDigests"]),
                "official base manifest/config/platform differs")
    require(image["inspection"]["Id"] == image["image_id"] and image["inspection"]["Architecture"] == "arm64"
            and image["inspection"]["Os"] == "linux", "derived image inspection differs")
    for name, version in (("source", "3.11.2"), ("native", "3.11.3")):
        probe = image["probes"][name]
        require(probe["implementation"] == "cpython" and probe["version"] == version
                and probe["isolated"] == probe["no_site"] == 1 and probe["dont_write_bytecode"] is True
                and re.fullmatch(r"[0-9a-f]{64}", probe["executable_sha256"]), "actual interpreter probe differs")
    require(image["probes"]["go"] == "go version go1.23.3 linux/arm64", "actual Go toolchain differs")
    require(api.check_ref(image["dockerfile"]).read_bytes() == (api.repo / "ci/go-port/assets/Dockerfile").read_bytes(), "derived Dockerfile differs")
    asset_pins = ASSETS | {"remote-go-image-resolution.json": "104ba2c09e218d92d5b44c40a781fc72adfa1e0e73d592bbf6969dffc3ecf2f2"}
    for name, expected in asset_pins.items():
        require(sha(api.check_ref(boot["assets"][name])) == expected
                and sha(api.repo / "ci/go-port/assets" / name) == expected, "fixed public asset changed")
    require(api.check_ref(boot["assets"]["Dockerfile"]).read_bytes() == (api.repo / "ci/go-port/assets/Dockerfile").read_bytes(), "bootstrap Dockerfile copy differs")
    wheels = read(api.repo / "ci/go-port/assets/linux-wheel-manifest.json")
    require(len(wheels) == len(boot["downloads"]) == 22 and [row["name"] for row in wheels] == [row["name"] for row in boot["downloads"]], "existing22 wheel inventory differs")
    for expected, download in zip(wheels, boot["downloads"]):
        metadata_path = api.check_ref(download["metadata"]["file"])
        metadata = read(metadata_path)
        origins = [(urlsplit(download[kind]["url"]), host) for kind, host in
                   (("metadata", "pypi.org"), ("wheel", "files.pythonhosted.org"))]
        require(all(parts.scheme == "https" and parts.netloc == host and not parts.fragment
                    for parts, host in origins), "nonpublic dependency origin")
        matches = [row for row in metadata["urls"] if row["filename"] == expected["name"]
                   and row["digests"]["sha256"] == expected["sha256"] and row["size"] == expected["bytes"]
                   and row["packagetype"] == "bdist_wheel" and row["url"] == download["wheel"]["url"]]
        require(len(matches) == 1 and download["wheel"]["file"]["sha256"] == expected["sha256"]
                and download["wheel"]["file"]["bytes"] == expected["bytes"], "wheel metadata/hash binding differs")
        if not api.artifact_check:
            api.check_ref(download["wheel"]["file"])
    for name in ("go.mod", "go.sum"):
        require(api.check_ref(boot["module_inputs"][name]).read_bytes() == (api.repo / "go" / name).read_bytes(), "module input differs")
    modules = boot["modules"]
    require(modules["approved_module"] == "github.com/mattn/go-sqlite3" and modules["version"] == "v1.14.52"
            and modules["manifest"], "public module preparation changed dependencies")
    if not api.artifact_check:
        module_root = api.local(modules["path"])
        for name, item in modules["manifest"].items():
            path = module_root / relative(name)
            require(not path.is_symlink() and path.stat().st_size == item["bytes"] and sha(path) == item["sha256"], "fresh module bytes differ")
    commands = {}
    for command in boot["commands"]:
        require(command["name"] not in commands and command["finished_utc"], "duplicate/incomplete bootstrap command")
        commands[command["name"]] = command
        api.check_ref(command["output"])
        require(command["exit_code"] == 0 or (command["name"].endswith("-owned-inspect") and command["exit_code"] == 1), "bootstrap command failed")
        if command["name"].endswith("-empty-check"):
            require(not api.local(command["output"]["path"]).read_text().strip(), "owned bootstrap container remained")
    for label, expected in (("derived-source-probe", image["probes"]["source"]), ("derived-native-probe", image["probes"]["native"]),
                            ("base-source-probe", image["base_images"]["go"]["source_probe"]),
                            ("base-native-probe", image["base_images"]["python"]["native_probe"])):
        require(json.loads(api.local(commands[label]["output"]["path"]).read_text()) == expected, "raw probe/output mismatch")
    require(api.local(commands["derived-go-probe"]["output"]["path"]).read_text().strip() == image["probes"]["go"]
            and "all modules verified" in api.local(commands["existing-module-download-verify"]["output"]["path"]).read_text(), "Go/module raw probe absent")
    build = commands["build-derived"]["argv"]
    require(build[:7] == ["docker", "build", "--platform", "linux/arm64", "--pull=false", "--no-cache", "--network=none"], "build resource/network origin differs")
    return image, {"bootstrap": api.ref(bootstrap_path), "image": api.ref(image_path), "base_images": image["base_images"],
                   "probes": image["probes"], "wheel_count": 22, "module": "github.com/mattn/go-sqlite3@v1.14.52",
                   "nontransported_file_checks": "Original CI verifies actual wheel/module/cache/workspace/dependency bytes; artifact --check revalidates retained raw proofs only."}


def archive_evidence(api, folder, record, manifest):
    current = {name: value for name, value in manifest.items() if name.startswith(("go/", "python/"))}
    before, after = read(folder / "archive-manifest.json"), read(folder / "archive-manifest.after.json")
    require(before == after == current, "stage before/after manifest differs from606")
    item = record["input"]
    require(item["revision"] == api.revision and item["files"] == len(current)
            and item["archive_sha256"] == sha(folder / "input.tar")
            and item["manifest_sha256"] == sha(folder / "archive-manifest.json"), "stage archive/revision binding differs")
    observed = {}
    with tarfile.open(folder / "input.tar") as archive:
        require(archive.pax_headers.get("comment") == api.revision, "Git archive commit comment differs")
        for entry in archive:
            require(entry.isdir() or entry.isfile(), "nonregular Git archive member")
            if entry.isfile():
                relative(entry.name)
                require(entry.name not in observed, "duplicate archive member")
                with archive.extractfile(entry) as content:
                    observed[entry.name] = hashlib.file_digest(content, "sha256").hexdigest()
    require(observed == current, "Git archive members differ from the exact runtime baseline")
    if not api.artifact_check:
        actual = {str(path.relative_to(folder / "workspace")): sha(path) for path in (folder / "workspace").rglob("*") if path.is_file()}
        require(actual == current, "actual post-stage workspace bytes differ")
    return {"archive": api.ref(folder / "input.tar"), "manifest": api.ref(folder / "archive-manifest.json"), "members": len(current)}


def stage_evidence(api, mode, manifest, image):
    folder = api.out / STAGES[mode]
    row = read(folder / "result.json")
    require(row.get("status") == ("source_captured" if mode == "source" else "passed") and row["mode"] == mode
            and row["revision"] == api.revision and row["runner_exit_code"] == row["stage_exit_code"] == 0
            and row["launched"] is True and row["finished_utc"] and row["inputs_unchanged"] is True
            and row["owned_resources_absent"] is True and row["cleanup_errors"] == [], "stage absent, failed, incomplete or not cleaned: " + mode)
    require(row["runner_sha256"] == sha(api.repo / "ci/go-port/linux_runner.py") and row["profile"] == PROFILE
            and row["host_pressure_helper_sha256"] == sha(api.repo / "ci/go-port/host_pressure.py"), "CI runner/helper/profile changed")
    require(row["configured_paths"] == {"repository": str(api.recorded_repo), "audit_output": str(api.recorded_out),
                "assets": str(api.recorded_out / "assets"), "module_cache": str(api.recorded_out / "modules")},
            "stage borrowed a different checkout/output/assets/module cache")
    require(row["image"] == image["image_id"] and api.check_ref(row["image_record"]) == api.out / "linux-image.json"
            and read(folder / "image-record.json") == image, "stage/derived image binding differs")
    actual_image = read(folder / "image.json")
    require(actual_image["Id"] == image["image_id"] and actual_image["Architecture"] == "arm64" and actual_image["Os"] == "linux", "stage image platform differs")
    limits = {"Memory": 3221225472, "MemorySwap": 3221225472, "NanoCpus": 2000000000,
              "PidsLimit": 256, "ReadonlyRootfs": True, "NetworkMode": "none"}
    require(row["container_limits"] == limits and row["container_state"]["ExitCode"] == 0
            and row["container_state"]["Running"] is False and row["container_state"]["OOMKilled"] is False, "container did not exit within fixed profile")
    created = read(folder / "container-created.json")
    terminal = read(folder / "container-before-cleanup.json")
    require(created["Id"] == terminal["Id"] and created["Config"]["Labels"]["io.mini-loop.cpython-linux.owner"] == row["owner_token"]
            and all(created["HostConfig"][key] == value for key, value in limits.items())
            and terminal["State"]["ExitCode"] == 0 and terminal["State"]["Running"] is False, "owned container identity/cleanup differs")
    environment = set(created["Config"]["Env"])
    require({"GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOMEMLIMIT=1GiB", "GOMAXPROCS=2"} <= environment
            and any(mount["Destination"] == "/gomod" and mount["RW"] is False for mount in created["Mounts"]),
            "native gate dependency/network/toolchain isolation differs")
    api.hashes(folder, row["log_hashes"])
    require(row["log_hashes"]["host-pressure-readings.jsonl"] == sha(folder / "host-pressure-readings.jsonl"), "PSI log not sealed")
    pressure = pressure_evidence(api, folder, row)
    cgroup = row["cgroup_memory_events"]
    require("final.memory.events" in cgroup and "final.memory.peak" in row["cgroup_peak_bytes"], "final cgroup measurements absent")
    for name, expected in cgroup.items():
        observed = {key: int(value) for key, value in (line.split() for line in (folder / "logs" / name).read_text().splitlines())}
        require(observed == expected and not any(expected.get(key, 0) for key in ("oom", "oom_kill", "oom_group_kill")), "OOM/raw event mismatch")
    for name, value in row["cgroup_peak_bytes"].items():
        require(int((folder / "logs" / name).read_text()) == value <= 3221225472, "cgroup peak differs/exceeds budget")
    archive = archive_evidence(api, folder, row, manifest)
    proof = read(folder / "input-proof.json")
    require(proof == row["input_proof"] and proof["source_code_transforms"] == proof["assertion_code_transforms"] == []
            and proof["input_hashes"] == {str(path.relative_to(folder / "inputs")): sha(path) for path in (folder / "inputs").rglob("*") if path.is_file()}
            and row["worker_script_sha256"] == sha(folder / "gates.sh"), "stage preparation/source transformations differ")
    plan = read(folder / "inputs/plan.json")
    require(proof["plan_sha256"] == sha(folder / "inputs/plan.json") and plan["baseline_commit"] == api.revision
            and plan["source_hashes"] == {name: value for name, value in manifest.items() if name.startswith("python/")}
            and plan["image"]["id"] == image["image_id"] and plan["image"]["python_version"] == "3.11.2"
            and plan["image"]["native_parser_version"] == "3.11.3", "plan/current source/interpreter binding differs")
    require(plan["current_outer_overrides"]["host_pressure_helper_sha256"] == row["host_pressure_helper_sha256"],
            "Source/native plan pressure helper differs")
    require(row["source_python_verified"] is True and read(folder / "logs/platform-context.json")["python"] == "3.11.2"
            and (folder / "logs/platform-context.exit").read_text().strip() == "0", "Source interpreter probe failed")
    commands = read(folder / "commands.json")
    require(commands["mode"] == mode and commands["selectors"] == [] and commands["automatic_retry"] is False
            and commands["coverage"] is False, "stage selector/retry scope differs")
    summary = {"stage": folder.name, "result": api.ref(folder / "result.json"), "status": row["status"], "seconds": row["seconds"],
               "archive": archive, "pressure": pressure, "cgroup_peak_bytes": row["cgroup_peak_bytes"],
               "owned_resources_absent": True, "no_oom": True, "raw_log_hashes": row["log_hashes"]}
    if mode == "source":
        capture = folder / "logs/source-capture"
        receipt = read(capture / "receipt.json")
        require(receipt == row["source_receipt"] and receipt["status"] == "captured_six_original_producers"
                and receipt["executed_full_export"] is False and [item["id"] for item in receipt["groups"]] == list(GROUPS)
                and all(item["exit_code"] == 0 and not item.get("timed_out") for item in receipt["groups"]), "Source groups incomplete")
        for name, item in receipt["files"].items():
            relative(name)
            path = capture / name
            require(Path(name).name == name and path.stat().st_size == item["bytes"] and sha(path) == item["sha256"], "captured Source bytes differ")
        environment = read(capture / "environment.json")
        require(environment["source_hashes"] == plan["source_hashes"] and environment["plan_sha256"] == proof["plan_sha256"]
                and environment["script_sha256"] == ASSETS["linux-source-capture.py"]
                and environment["dependencies"] == {item["name"]: item["version"] for item in plan["dependencies"]["capture_required"]},
                "actual Source provenance/dependency versions differ")
        require(sha(folder / "dependency-manifest.json") == proof["dependency_manifest_sha256"], "Source dependency manifest changed")
        if not api.artifact_check:
            api.hashes(folder / "deps", read(folder / "dependency-manifest.json"))
        inventories = []
        for group, count in zip(GROUPS, GROUP_COUNTS):
            recipe = plan["groups"][group]
            path = capture / recipe["fixture"]
            data = read(path)
            names = [case["name"] for case in data["cases"]]
            context, producer = read(capture / (group + ".context.json")), read(capture / (group + ".producer.json"))
            require(names == recipe["expected_all_cases"] == context["captured_cases"] and len(names) == count
                    and sorted(data) == recipe["expected_top_level_keys"] == context["top_level_keys"]
                    and producer["producer_path"] == recipe["source"]["path"] and producer["symbol"] == recipe["source"]["symbol"]
                    and producer["line"] == recipe["source"]["line"]
                    and producer["producer_sha256"] == manifest[producer["producer_path"]] and producer["source_transforms"] == []
                    and hashlib.sha256(producer["exact_source"].encode()).hexdigest() == producer["function_sha256"], "Source original recipe inventory differs")
            inventories.append({"id": group, "count": count, "names": names, "fixture": api.ref(path)})
        require(commands["native_parser_probe"] is None and commands["package_inventory"] is None
                and "native_parser_verified" not in row and commands["argv"] == ["nice", "-n", "10", "/usr/bin/python3", "-B", "/capture.py", "--source-root", "/workspace", "--plan", "/plan.json", "--output", "/out/source-capture"], "Source/native command conflation")
        summary.update(groups=inventories, group_count=6, recipe_count=66, source_python="3.11.2", full_python_export=False)
        return summary, None
    source = api.out / STAGES["source"]
    capture = proof["source_capture"]
    captured = source / "logs/source-capture"
    require(api.local(capture["capture_root"]) == captured and capture["receipt_sha256"] == sha(captured / "receipt.json")
            and capture["outer_result_sha256"] == sha(source / "result.json")
            and capture["archive_sha256"] == row["input"]["archive_sha256"], "Source/native stage linkage differs")
    api.hashes(captured, capture["fixture_hashes"])
    require(row["source_platform_context_matches"] is True and row["fixture_hashes_after"] == capture["fixture_hashes"]
            and set(capture["fixture_hashes"]) == {plan["groups"][group]["fixture"] for group in GROUPS}, "native overlays differ")
    native_context = read(folder / "logs/platform-context.json")
    source_context = read(captured / "environment.json")
    require(native_context["shell"] == source_context["shell"] and native_context["git"] == source_context["git"]["stdout"].strip()
            and native_context["tmp_filesystem"] == source_context["tmp"]["stat"]["stdout"].strip()
            and native_context["uname"][0] == source_context["uname"][0]
            and native_context["uname"][2:5] == source_context["uname"][2:5], "raw Source/native platform context differs")
    parser = read(folder / "logs/native-parser-context.json")
    require(row["native_parser_verified"] is True and row["native_parser_probe_exit_code"] == 0 and parser == row["native_parser_context"]
            and parser["implementation"] == "cpython" and parser["version"] == "3.11.3" and parser["invoked_path"] == "/usr/local/bin/python3.11"
            and parser["executable_sha256"] == image["probes"]["native"]["executable_sha256"]
            and parser["required_imports"] == ["ast", "base64", "json", "resource"] and parser["isolated"] == parser["no_site"] == 1
            and parser["dont_write_bytecode"] is True and (folder / "logs/native-parser-context.exit").read_text().strip() == "0", "actual native3.11.3 probe differs")
    packages = (folder / "logs/packages.txt").read_text().splitlines()
    go = row["go_results"]
    require(set(packages) == PACKAGES and len(packages) == 53 and go["expected_packages"] == packages
            and go["listing_exit_code"] == 0 and go["complete_scope"] is True, "complete53 package scope absent")
    unrestricted(commands["argv"], mode)
    require(commands["package_inventory"] == ["nice", "-n", "10", "go", "list", "-p=1", "./..."], "package inventory command narrowed")
    summary.update(package_count=53, native_parser=parser)
    return summary, go


def go_evidence(api, mode, declared, manifest, required):
    path = api.out / STAGES[mode] / "logs/stdout.log"
    terminals, packages, counts = {}, {}, Counter()
    optional = {
        ("cron", "TestClaimProcessHelper"): "subprocess helper",
        ("sqlitestate", "TestNativeSQLiteCrashProcessHelper"): "subprocess helper",
        ("httpapi", "TestActualSourceRoutingAudit"): "routing audit output was not requested",
        ("provider", "TestProviderPartialInputAudit"): "diagnostic provider audit output was not requested",
        ("provider", "TestProviderURLScopeAudit"): "URL scope diagnostic output was not requested",
        ("provider", "TestProviderUsageConsumerAudit"): "usage consumer diagnostic output was not requested",
        ("launcher", "TestLauncherDiscoveryMatchesSourceConstructorsAndManagedTurns/profile-federation-force"):
            "same-client forced invalidation is covered by provider tests; Launcher exposes no invalidation handle",
    }
    optional = {(MODULE + "/" + package, test): reason for (package, test), reason in optional.items()}
    reasons, no_tests = {}, set()
    for line, row in stream_json(path):
        action, package, test = row.get("Action"), row.get("Package"), row.get("Test")
        require(isinstance(action, str), "invalid Go event action")
        identity = (package, test)
        if action == "output":
            output = row.get("Output", "")
            if identity in optional and optional[identity] in output:
                reasons.setdefault(identity, []).append({"line": line, "output": output})
            if package in (MODULE + "/internal/fnmatch", MODULE + "/runtimeconfig") and re.fullmatch(
                    r"\?\s+" + re.escape(package) + r"\s+\[no test files\]\s*", output):
                no_tests.add(package)
        if action not in ("pass", "skip", "fail"):
            continue
        require(package in PACKAGES and identity not in terminals, "unknown/duplicate terminal Go identity")
        terminals[identity] = action
        scope = ("subtest" if "/" in test else "top_level") if test else "package"
        counts[scope + "_" + action] += 1
        if test is None:
            packages[package] = action
    require(set(packages) == PACKAGES and packages == declared["package_terminal"]
            and not any(value for name, value in counts.items() if name.endswith("_fail")), "raw package set/count/failure differs")
    for action in ("pass", "skip", "fail"):
        require(counts["top_level_" + action] == declared["test_counts"][action]
                and counts["subtest_" + action] == declared["subtest_counts"][action], "raw Go test count differs")
    package_skips = []
    test_skips = []
    for (package, test), action in terminals.items():
        if action != "skip":
            continue
        if test is None:
            require(package in no_tests, "skipped package lacks witnessed no-test output")
            directory = "go/" + package[len(MODULE) + 1:]
            sources = [name for name in manifest if str(Path(name).parent) == directory and name.endswith(".go")]
            require(sources and not any(name.endswith("_test.go") for name in sources)
                    and not any(other == package and named is not None for other, named in terminals), "real-test package was skipped")
            package_skips.append({"package": package, "raw_reason": "[no test files]", "source_files": sources})
        else:
            require((package, test) in optional and reasons.get((package, test)) and (package, test) not in required,
                    "unreviewed or required named test skipped")
            test_skips.append({"package": package, "test": test, "terminal_retained": "skip", "raw_reason_events": reasons[(package, test)]})
    missing = sorted(identity for identity in required if terminals.get(identity) != "pass")
    require(not missing, "required semantic/core/persistence pass absent: " + repr(missing[:5]))
    return {"raw": api.ref(path), "package_count": 53, "package_terminal_counts": dict(Counter(packages.values())),
            "counts": dict(counts), "no_test_package_skips": package_skips, "actual_test_skips": test_skips,
            "required_named_semantics_passed": len(required), "required_named_semantics_skipped_or_failed": 0}


def publish(path, value):
    require(not path.exists(), "refuse overwriting existing verification")
    temporary = path.parent / (".verification-" + uuid.uuid4().hex + ".tmp")
    try:
        with temporary.open("xb") as stream:
            stream.write((json.dumps(value, sort_keys=True, indent=2, ensure_ascii=False) + "\n").encode())
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--recorded-out", type=Path, help="explicit original CI OUT prefix when checking extracted artifacts")
    parser.add_argument("--recorded-repo-root", type=Path, help="explicit original checkout prefix when checking extracted artifacts")
    parser.add_argument("--check", action="store_true", help="read-only recheck of retained artifact evidence, not omitted caches")
    args = parser.parse_args()
    try:
        api = Evidence(args)
        require(len(PACKAGES) == len(PACKAGE_SUFFIXES) == 53, "review fixed53 package inventory")
        manifest, runtime = runtime_inputs(api)
        required, semantic = required_semantics(api, manifest)
        image, preparation = bootstrap_evidence(api)
        stages = {}
        for mode in ("source", "full", "vet", "race"):
            stages[mode], declared = stage_evidence(api, mode, manifest, image)
            if mode in ("full", "race"):
                stages[mode]["go_events"] = go_evidence(api, mode, declared, manifest, required)
        receipt = {"schema_version": 1, "status": "complete_current_runtime_linux_ci_evidence_verified",
                   "created_utc": datetime.now(timezone.utc).isoformat(), "runtime": runtime,
                   "workflow_commit": args.revision,
                   "github_context": {name: os.environ.get(name) for name in ("GITHUB_SHA", "GITHUB_WORKFLOW_SHA", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_REPOSITORY", "GITHUB_WORKFLOW_REF")},
                   "recorded_roots": {"repository": str(api.recorded_repo), "audit_output": str(api.recorded_out)},
                   "bootstrap": preparation, "semantic_scope": semantic, "stages": stages,
                   "required_stage_count": 4, "full_port_acceptance": False, "required_row_delta_applied": False,
                   "overall_goal_complete": False,
                   "ci_only_file_verification": {"wheel_module_cache_workspace_and_deps_bytes_checked_before_pack": True,
                       "artifact_recheck_does_not_revalidate_omitted_files": True},
                   "limitations": ["Only the exact original bounded Source/native contracts and named typed adaptations are verified.",
                       "Source3.11.2 six-group capture is separate from native3.11.3/62 parser execution.",
                       "MockTransport IDNA84 does not prove live-vendor DNS/SNI or universal wire equivalence.",
                       "Bidi2614 assertions execute inside one passing named test; they are not2614 terminal events.",
                       "Cgroup charged memory excludes host overhead; process RSS and host PSI are sampled.",
                       "This CI receipt does not update the current344 ledger or grant final acceptance."]}
        target = api.out / "verification.json"
        if args.check:
            original = read(target)
            require(original["status"] == receipt["status"] and original["workflow_commit"] == args.revision
                    and original["runtime"] == runtime and original["semantic_scope"] == semantic
                    and original["stages"] == stages and original["bootstrap"] == preparation
                    and original["ci_only_file_verification"]["wheel_module_cache_workspace_and_deps_bytes_checked_before_pack"] is True
                    and original["full_port_acceptance"] is False and original["required_row_delta_applied"] is False,
                    "original CI verification/retained artifact evidence differs")
            print(json.dumps({"status": "retained_artifact_evidence_revalidated", "verification": api.ref(target),
                              "omitted_file_bytes_reverified": False, "full_port_acceptance": False}))
        else:
            publish(target, receipt)
            print(json.dumps({"status": receipt["status"], "verification": api.ref(target)}))
        return 0
    except (OSError, ValueError, KeyError, TypeError, ArithmeticError, subprocess.SubprocessError) as error:
        print(json.dumps({"status": "verification_failed", "error": type(error).__name__ + ": " + str(error),
                          "full_port_acceptance": False, "required_row_delta_applied": False}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
