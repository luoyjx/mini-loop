"""Observe real AST adapter admission and version-probe subprocesses."""

from dataclasses import asdict
import hashlib
from pathlib import Path
import sys
import tempfile

from mini_loop.ast_context import AstContextConfig, AstContextProbe, AstContextResult, AstOutlineAdapter


def ast_context_contracts() -> dict:
    configurations = []
    recipes = [
        ("defaults", {}),
        ("empty-binary", {"binary": ""}),
        ("zero-timeout", {"timeout_seconds": 0}),
        ("negative-timeout", {"timeout_seconds": -1}),
        ("zero-output", {"max_output_bytes": 0}),
        ("negative-output", {"max_output_bytes": -1}),
        ("deduplicated-env", {"env_passthrough": ["LANG", "AST_TEST", "LANG"]}),
        ("empty-env", {"env_passthrough": [""]}),
        ("invalid-env", {"env_passthrough": ["PATH=secret"]}),
        ("numeric-env", {"env_passthrough": [42]}),
        ("normalised-digest", {"expected_sha256": " " + "AB" * 32 + "\n"}),
        ("short-digest", {"expected_sha256": "ab"}),
        ("nonhex-digest", {"expected_sha256": "z" * 64}),
        ("wrong-schema", {"schema_version": 2}),
        ("bad-version", {"min_version": "v1.9.0"}),
        ("empty-range", {"min_version": "1.10.0"}),
        ("suffix-range", {"min_version": "1.9.0+build", "max_version_exclusive": "1.10.0-rc1"}),
        ("unicode-version", {"min_version": "١.٩.٠"}),
    ]
    for name, values in recipes:
        row = {"name": name, "input": values}
        try:
            row["output"] = asdict(AstContextConfig(**values))
        except Exception as error:
            row["error_class"] = type(error).__name__
            row["error"] = str(error)
        configurations.append(row)

    probes = []
    with tempfile.TemporaryDirectory(prefix="mini-loop-ast-contract-") as temporary:
        root = Path(temporary)
        workspace = root / "workspace"
        workspace.mkdir()
        marker = root / "calls"
        homepage = "https://github.com/ast-outline/ast-outline"
        versions = [
            ("compatible", f"ast-outline 1.9.0\nhomepage: {homepage}\n", "", 0),
            ("latest-patch", f"ast-outline 1.9.999\nhomepage: {homepage}\n", "", 0),
            ("prerelease", f"ast-outline 1.9.1-rc1+build\nhomepage: {homepage}/\n", "", 0),
            ("lower-bound", f"ast-outline 1.8.9\nhomepage: {homepage}\n", "", 0),
            ("upper-bound", f"ast-outline 1.10.0\nhomepage: {homepage}\n", "", 0),
            ("wrong-homepage", "ast-outline 1.9.0\nhomepage: https://example.invalid\n", "", 0),
            ("missing-homepage", "ast-outline 1.9.0\n", "", 0),
            ("missing-version", f"homepage: {homepage}\n", "", 0),
            ("prefixed-version", f"prefix ast-outline 1.9.0\nhomepage: {homepage}\n", "", 0),
            ("multiline-search", f"banner\nast-outline 1.9.2\nhomepage: {homepage}\nextra\n", "", 0),
            ("unicode-version", f"ast-outline ١.٩.٠\nhomepage: {homepage}\n", "", 0),
            ("nonzero-exit", "", "  version failure\n", 7),
        ]
        for name, stdout, stderr, exit_code in versions:
            binary = root / "ast-probe"
            binary.write_text(
                f"#!{sys.executable}\n"
                "from pathlib import Path\nimport sys\n"
                f"Path({str(marker)!r}).write_text('called')\n"
                f"sys.stdout.write({stdout!r})\nsys.stderr.write({stderr!r})\n"
                f"sys.exit({exit_code})\n"
            )
            binary.chmod(0o700)
            marker.unlink(missing_ok=True)
            result = AstOutlineAdapter(AstContextConfig(binary=binary)).probe(cwd=workspace)
            probes.append({"name": name, "stdout": stdout, "stderr": stderr,
                           "exit_code": exit_code, "output": result.as_dict(),
                           "executed": marker.exists()})

        for name in ("missing", "workspace-binary", "digest-mismatch", "digest-match"):
            binary = root / "ast-probe"
            digest = hashlib.sha256(binary.read_bytes()).hexdigest()
            if name == "missing":
                binary = root / "absent"
            elif name == "workspace-binary":
                inside = workspace / "ast-probe"
                inside.write_bytes(binary.read_bytes())
                inside.chmod(0o700)
                binary = inside
            marker.unlink(missing_ok=True)
            expected = "0" * 64 if name == "digest-mismatch" else digest if name == "digest-match" else None
            result = AstOutlineAdapter(AstContextConfig(binary=binary, expected_sha256=expected)).probe(cwd=workspace)
            output = result.as_dict()
            if "message" in output:
                output["message"] = output["message"].replace(str(root), "<scratch>")
            probes.append({"name": name, "output": output, "executed": marker.exists()})

    projections = []
    for status in ("applied", "no_match", "partial", "ambiguous", "error", "missing", "incompatible", "unknown"):
        row = {"status": status}
        try:
            probe = AstContextProbe(status, version="1.9.0", message="probe detail")
            result = AstContextResult(status, "repo_map", data={"schema_version": 1, "files": []},
                                      probe=probe, warnings=("warning",), message="detail", output_bytes=17)
            row["probe"] = probe.as_dict()
            row["result"] = result.as_dict()
            row["json"] = result.to_json()
            row["minimal"] = AstContextResult(status, "show_symbol").as_dict()
        except Exception as error:
            row["error_class"] = type(error).__name__
            row["error"] = str(error)
        projections.append(row)
    return {"configurations": configurations, "probes": probes, "projections": projections}
