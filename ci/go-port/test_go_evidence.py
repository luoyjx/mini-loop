"""Small synthetic-log tests for CI skip admission; never start Go or Node."""
from collections import Counter
import importlib.util
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("go_port_verify_under_test", Path(__file__).with_name("verify.py"))
VERIFY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERIFY)


class GoEvidenceSkipTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.out = Path(self.temporary.name)
        self.api = VERIFY.Evidence(SimpleNamespace(repo_root=ROOT, out=self.out, revision="a" * 40,
                                                  recorded_repo_root=None, recorded_out=None, check=True))
        self.manifest = VERIFY.read(ROOT / VERIFY.HOST)["runtime_inputs"]["manifest"]
        self.required = set(VERIFY.LINUX_UNAVAILABLE) | {VERIFY.BROWSER_INTERACTION}

    def evidence(self, *, source="Linux", native="Linux", extra_skip=None, missing=None, altered_reason=False):
        source_file = self.out / VERIFY.STAGES["source"] / "logs/source-capture/environment.json"
        native_file = self.out / VERIFY.STAGES["full"] / "logs/platform-context.json"
        for path, system in ((source_file, source), (native_file, native)):
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps({"uname": [system]}))
        rows = []
        for identity in sorted(VERIFY.SEATBELT_SKIPS):
            package, test = identity
            reason = VERIFY.SEATBELT_REASON + (" changed" if altered_reason else "")
            rows.extend([{"Action": "output", "Package": package, "Test": test,
                          "Output": "    " + Path(VERIFY.SEATBELT_SKIPS[identity]).name + ":1: " + reason + "\n"},
                         {"Action": "skip", "Package": package, "Test": test}])
        for package, test in sorted(self.required):
            if (package, test) != missing:
                action = "skip" if (package, test) == extra_skip else "pass"
                rows.append({"Action": action, "Package": package, "Test": test})
        if extra_skip is not None and extra_skip not in self.required:
            package, test = extra_skip
            rows.extend([{"Action": "output", "Package": package, "Test": test,
                          "Output": "    execution_test.go:1: " + VERIFY.SEATBELT_REASON + "\n"},
                         {"Action": "skip", "Package": package, "Test": test}])
        rows.extend({"Action": "pass", "Package": package} for package in sorted(VERIFY.PACKAGES))
        path = self.out / VERIFY.STAGES["full"] / "logs/stdout.log"
        path.write_text("".join(json.dumps(row) + "\n" for row in rows))
        counts = {scope: Counter() for scope in ("test_counts", "subtest_counts")}
        for row in rows:
            if row["Action"] in ("pass", "skip", "fail") and row.get("Test"):
                scope = "subtest_counts" if "/" in row["Test"] else "test_counts"
                counts[scope][row["Action"]] += 1
        declared = {"package_terminal": dict.fromkeys(VERIFY.PACKAGES, "pass")}
        declared.update({scope: dict.fromkeys(("pass", "skip", "fail"), 0) | dict(values) for scope, values in counts.items()})
        return declared

    def check(self, declared, required=None):
        return VERIFY.go_evidence(self.api, "full", declared, self.manifest,
                                  self.required if required is None else required)

    def test_six_exact_linux_skips_preserve_reasons_and_pending_host_proof(self):
        result = self.check(self.evidence())
        self.assertEqual(len(result["actual_test_skips"]), 6)
        self.assertTrue(all(row["terminal_retained"] == "skip" and len(row["raw_reason_events"]) == 1
                            for row in result["actual_test_skips"]))
        proof = result["darwin_only_seatbelt_adapter"]
        self.assertEqual(proof["host_receipt"]["sha256"], VERIFY.HOST_SHA)
        self.assertEqual(len(proof["six_darwin_test_identities"]), 6)
        self.assertFalse(proof["historical_darwin_raw_passes_revalidated_here"])

    def test_both_actual_platform_contexts_must_be_linux(self):
        for source, native in (("Darwin", "Linux"), ("Linux", "Darwin"), ("Darwin", "Darwin")):
            with self.subTest(source=source, native=native), self.assertRaisesRegex(ValueError, "actual Linux"):
                self.check(self.evidence(source=source, native=native))

    def test_platform_reason_must_match_whole_message(self):
        with self.assertRaisesRegex(ValueError, "unreviewed or required"):
            self.check(self.evidence(altered_reason=True))

    def test_same_reason_does_not_admit_another_seatbelt_name(self):
        unknown = (VERIFY.MODULE + "/sandbox", "TestActualDefaultSeatbeltReadWriteAndRebindExtra")
        with self.assertRaisesRegex(ValueError, "unreviewed or required"):
            self.check(self.evidence(extra_skip=unknown))

    def test_even_a_platform_skip_cannot_replace_a_required_pass(self):
        with self.assertRaisesRegex(ValueError, "unreviewed or required"):
            self.check(self.evidence(), self.required | {next(iter(VERIFY.SEATBELT_SKIPS))})

    def test_browser_interaction_skip_is_never_optional(self):
        with self.assertRaisesRegex(ValueError, "unreviewed or required"):
            self.check(self.evidence(extra_skip=VERIFY.BROWSER_INTERACTION))

    def test_linux_unavailable_skip_is_never_optional(self):
        with self.assertRaisesRegex(ValueError, "unreviewed or required"):
            self.check(self.evidence(extra_skip=next(iter(VERIFY.LINUX_UNAVAILABLE))))

    def test_missing_linux_unavailable_pass_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "required semantic/core/persistence pass absent"):
            self.check(self.evidence(missing=next(iter(VERIFY.LINUX_UNAVAILABLE))))


class NodeEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.out = Path(self.temporary.name).resolve(strict=True)
        self.api = VERIFY.Evidence(SimpleNamespace(repo_root=ROOT, out=self.out, revision="a" * 40,
                                                  recorded_repo_root=None, recorded_out=None, check=False))
        self.source = self.out / "existing-runner-node"
        self.source.write_bytes(b"synthetic executable bytes; never executed")
        copied = self.out / "build-context/node"
        copied.parent.mkdir()
        copied.write_bytes(self.source.read_bytes())
        size, digest = self.source.stat().st_size, VERIFY.sha(self.source)
        source = {"reported_path": str(self.source), "bytes": size, "sha256": digest}
        probe = {"version": "v22.23.3", "platform": "linux", "arch": "arm64", "execPath": str(self.source), "bytes": size, "sha256": digest}
        self.node = {"selected_path": str(self.source), "source": source, "source_after": source.copy(),
            "host_probe": probe, "derived_probe": probe | {"execPath": "/usr/local/bin/node"},
            "build_copy": self.api.ref(copied), "source_unchanged": True, "copy_matches_source": True,
            "inventory_source": VERIFY.NODE_INVENTORY, "binary_transported": False, "transport_scope": VERIFY.NODE_TRANSPORT_SCOPE}
        self.commands = {}
        for name, key, argv in (("host-node-probe", "host_probe", [str(self.source), "--eval", "fixed-probe"]),
                                ("derived-node-probe", "derived_probe", ["docker", "/usr/local/bin/node", "--eval", "fixed-probe"])):
            path = self.out / (name + ".log")
            path.write_text(json.dumps(self.node[key]))
            self.commands[name] = {"argv": argv, "output": self.api.ref(path)}

    def check(self):
        return VERIFY.node_evidence(self.api, {"node": self.node}, {"node": self.node}, self.commands)

    def test_normal_verification_checks_actual_source_and_copy(self):
        self.assertEqual(self.check(), self.node)
        self.source.write_bytes(b"changed")
        with self.assertRaisesRegex(ValueError, "actual runner Node source changed"):
            self.check()

    def test_artifact_verification_uses_retained_probes_without_omitted_binary(self):
        self.api.local(self.node["build_copy"]["path"]).unlink()
        self.source.unlink()
        self.api.artifact_check = True
        self.assertEqual(self.check(), self.node)
        path = self.api.local(self.commands["derived-node-probe"]["output"]["path"])
        path.write_text("{}")
        with self.assertRaisesRegex(ValueError, "raw Node probes/commands differ"):
            self.check()

    def test_wrong_version_or_derived_hash_cannot_satisfy_node_requirement(self):
        self.node["derived_probe"]["version"] = "v22.23.2"
        with self.assertRaisesRegex(ValueError, "actual Node22.23.3"):
            self.check()
        self.node["derived_probe"]["version"] = "v22.23.3"
        self.node["derived_probe"]["sha256"] = "0" * 64
        with self.assertRaisesRegex(ValueError, "actual Node22.23.3"):
            self.check()


if __name__ == "__main__":
    unittest.main()
