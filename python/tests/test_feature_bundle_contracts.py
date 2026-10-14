"""Keep full-feature composition evidence deterministic and override-aware."""

import json
from pathlib import Path

from tools.feature_bundle_contracts import feature_bundle_contracts


def test_feature_bundle_contracts_match_export_across_roots(tmp_path):
    expected = json.loads(
        (Path(__file__).parents[2] / "go/testdata/python-feature-bundle.json").read_text()
    )
    assert feature_bundle_contracts(tmp_path / "first") == expected
    assert feature_bundle_contracts(tmp_path / "second") == expected


def test_feature_bundle_composition_and_overrides(tmp_path):
    fixture = feature_bundle_contracts(tmp_path)
    definitions = {item["definition_id"]: item for item in fixture["definitions"]}
    registries = {item["name"]: item for item in fixture["registries"]}
    managers = {item["name"]: item for item in fixture["managers"]}
    assert len(registries) == 14
    assert len(managers) == 8
    assert len(definitions) == 57
    assert all(definition["handler_bound"] for definition in definitions.values())
    assert len(registries["default"]["tools"]) == 10
    full = registries["full"]["tools"]
    assert len(full) == 48
    assert managers["full"]["tools"] == full
    assert managers["full-custom-tools"]["tools"] == registries["default"]["tools"]
    assert managers["full-custom-injectors"]["tools"] == full
    assert managers["full-custom-injectors"]["agent_injectors"] == [
        "steering_injector", "posture_injector", "runtime_facts_injector"
    ]
    assert managers["full"]["agent_injectors"] == [
        *fixture["default_injectors"],
        "steering_injector", "posture_injector", "runtime_facts_injector"
    ]
    for name, count in [("full-workflows", 51), ("full-ast", 52),
                        ("full-decisions", 49), ("full-independent", 56)]:
        assert len(managers[name]["tools"]) == count
        assert managers[name]["tools"][:48] == full
    for name, case in registries.items():
        selection = case["selection"]
        selected = selection["groups"]
        assert len(selected) == 12
        if name == "default":
            assert selection["kind"] == "default"
            assert not any(selected.values())
        else:
            assert selection["kind"] == "full"
            omitted = name.removeprefix("without-") if name.startswith("without-") else None
            assert all(enabled == (group != omitted) for group, enabled in selected.items())
        if name.startswith("without-"):
            names = [definitions[ref]["name"] for ref in case["tools"]]
            full_names = [definitions[ref]["name"] for ref in full]
            assert len(names) < len(full_names)
            assert names == [name for name in full_names if name in names]
        bash = next(definitions[ref] for ref in case["tools"] if definitions[ref]["name"] == "bash")
        assert bash["modes"] == ["exclusive", "parallel" if selected["background"] else "exclusive"]
    for name, case in managers.items():
        assert case["bindings"] == {"manager": True, "session": True}
        selection = case["selection"]
        assert selection["enable_features"] == (name != "off")
        assert selection["custom_tools"] == (name == "full-custom-tools")
        assert selection["custom_injectors"] == (name == "full-custom-injectors")
        assert selection["workflows"] == (name in ("full-workflows", "full-independent"))
        assert selection["ast"] == (name in ("full-ast", "full-independent"))
        assert selection["decisions"] == ("llm" if name in ("full-decisions", "full-independent") else "off")
    assert definitions["bash"]["modes"] == ["exclusive", "exclusive"]
    assert definitions["bash:variant-2"]["modes"] == ["exclusive", "parallel"]
    assert fixture["no_default_injectors"] == []
