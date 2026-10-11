"""Source evidence for the native sandbox port, independent of host availability."""

import json
from pathlib import Path

from tools.sandbox_contracts import sandbox_contracts


def test_sandbox_contract_export_is_stable_across_roots(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-sandbox.json").read_text())
    assert sandbox_contracts(tmp_path / "first") == expected
    assert sandbox_contracts(tmp_path / "second") == expected


def test_sandbox_contract_preserves_authority_and_rebinding(tmp_path):
    contracts = sandbox_contracts(tmp_path)
    cases = {case["name"]: case for case in contracts["cases"]}
    assert contracts["factory_defaults"]["unreadable"] == contracts["credential_roots"]
    assert not contracts["factory_defaults"]["allow_network"]
    quoted = cases["policy-syntax-path"]["before"]
    assert 'quote" (allow default)' not in quoted["argv"][2]
    assert '(require-not (literal (param "NOREAD_0")))' in quoted["argv"][2]
    assert '(require-not (subpath (param "NOREAD_0")))' in quoted["argv"][2]
    assert quoted["argv"][-3:] == ["/bin/sh", "-c", contracts["command"]]
    rebound = cases["duplicates-extra"]["after"]
    assert "<ROOT>/ordinary" not in rebound["writable"]
    assert "<ROOT>/extra" in rebound["writable"]
    assert "<ROOT>/workspace" in rebound["writable"]
    linked = cases["linked-worktree"]
    assert linked["before"]["writable"] == ["<ROOT>/linked", "<ROOT>/repo/.git"]
    assert linked["after"]["writable"] == ["<ROOT>/alternate", "<ROOT>/other/.git"]
    for selection in contracts["selections"]:
        if selection["system"] != "Darwin" or not selection["binary_exists"]:
            if selection["require"]:
                assert selection["error"]["kind"] == "SandboxUnavailable"
            else:
                assert selection["result"]["kind"] == "UnavailableSandbox"
                assert not selection["result"]["confined"]
