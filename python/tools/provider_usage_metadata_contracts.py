"""Observe known metadata shapes through the pinned SDK, without schema mocks."""
import hashlib
import inspect

from anthropic.types import CacheCreation, OutputTokensDetails, ServerToolUsage, Usage
from anthropic._models import BaseModel, construct_type, _construct_field

from tools.provider_usage_contracts import provider_usage_contracts


def provider_usage_metadata_contracts() -> dict:
    shapes = [
        ("null", None), ("integer", 13), ("fraction", 13.5),
        ("boolean", True), ("text", "secret-marker"), ("array", []),
        ("nested-array", [{"secret-marker": ["secret-marker", 9223372036854775808, 1e100]}, False]),
        ("object", {}), ("extension-object", {"secret-marker": ["secret-marker", 9223372036854775808, 1e100]}),
    ]
    core = {"input_tokens": "13", "output_tokens": 5}
    profiles = [
        {"name": f"{field}-{name}", "stream": True, "usage": {**core, field: value}}
        for field in ("cache_creation", "output_tokens_details", "server_tool_use", "service_tier", "inference_geo")
        for name, value in shapes
    ]
    for field, names in [
        ("cache_creation", ("ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens")),
        ("output_tokens_details", ("thinking_tokens",)),
        ("server_tool_use", ("web_fetch_requests", "web_search_requests")),
    ]:
        for label, values in [
            ("complete", {name: "7" for name in names}),
            ("null-count", {name: None for name in names}),
            ("fraction-count", {name: 1.5 for name in names}),
            ("array-count", {name: [] for name in names}),
        ]:
            profiles.append({"name": f"{field}-{label}", "stream": True,
                             "usage": {**core, field: values}})
        for name in names:
            profiles.append({"name": f"{field}-missing-{name}", "stream": True,
                             "usage": {**core, field: {key: "7" for key in names if key != name}}})
    for value in ("standard", "priority", "batch", "future", "STANDARD", ""):
        profiles.append({"name": f"service-tier-literal-{value or 'empty'}", "stream": True,
                         "usage": {**core, "service_tier": value}})
    profiles.append({"name": "inference-geo-string", "stream": True,
                     "usage": {**core, "inference_geo": "us"}})
    for name, value in shapes:
        profiles.append({"name": f"server-delta-{name}", "modes": ["stream"],
                         "usage": core, "delta_usage": {"output_tokens": 5, "server_tool_use": value}})
        profiles.append({"name": f"server-replace-{name}", "modes": ["stream"],
                         "usage": {**core, "server_tool_use": value},
                         "delta_usage": {"output_tokens": 5, "server_tool_use": {
                             "web_fetch_requests": "7", "web_search_requests": "11"}}})
    for field, names in [
        ("cache_creation", ("ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens")),
        ("output_tokens_details", ("thinking_tokens",)),
        ("server_tool_use", ("web_fetch_requests", "web_search_requests")),
    ]:
        complete = {name: "7" for name in names}
        for label, value in [
            ("array-complete", [complete]),
            ("array-partial", [{names[0]: "7"}]),
            ("array-nested-list", [[complete]]),
        ]:
            profiles.append({"name": f"{field}-{label}", "stream": True,
                             "usage": {**core, field: value}})
    for name, metadata in [
        ("invalid-tier-valid-cache", {"service_tier": "future", "cache_creation": {
            "ephemeral_1h_input_tokens": "7", "ephemeral_5m_input_tokens": "11"}}),
        ("invalid-tier-valid-server", {"service_tier": [], "server_tool_use": {
            "web_fetch_requests": "7", "web_search_requests": "11"}, "inference_geo": "us"}),
        ("partial-cache-valid-other", {"cache_creation": {"ephemeral_1h_input_tokens": "7"},
            "output_tokens_details": {"thinking_tokens": "3"}, "cache_read_input_tokens": "19"}),
        ("null-cache-valid-other", {"cache_creation": {"ephemeral_1h_input_tokens": "7", "ephemeral_5m_input_tokens": None},
            "cache_creation_input_tokens": "17", "cache_read_input_tokens": "19", "service_tier": "standard"}),
        ("null-server-valid-tier", {"server_tool_use": {"web_fetch_requests": "7", "web_search_requests": None},
            "service_tier": "standard", "inference_geo": "us"}),
    ]:
        profiles.append({"name": name, "stream": True, "usage": {**core, **metadata}})
    for name, value in [
        ("array", []), ("object", {}), ("boolean", True),
        ("complete", {"web_fetch_requests": "7", "web_search_requests": "11"}),
        ("array-complete", [{"web_fetch_requests": "7", "web_search_requests": "11"}]),
    ]:
        profiles.append({"name": f"server-delta-text-output-{name}", "modes": ["stream"],
                         "usage": core, "delta_usage": {"output_tokens": "5", "server_tool_use": value}})
    profiles.append({"name": "legacy-nontext-geo-assertion", "stream": True,
                     "usage": {"input_tokens": 1, "output_tokens": 2, "inference_geo": 5}})
    result = provider_usage_contracts(profiles=profiles, capture_consumers=True)
    models = (Usage, CacheCreation, OutputTokensDetails, ServerToolUsage)
    result["metadata_annotations"] = {
        model.__name__: {name: str(field.annotation) for name, field in model.model_fields.items()}
        for model in models
    }
    result["source_hashes"].update({
        model.__qualname__: hashlib.sha256(inspect.getsource(model).encode()).hexdigest()
        for model in models
    })
    result["source_hashes"].update({
        symbol.__qualname__: hashlib.sha256(inspect.getsource(symbol).encode()).hexdigest()
        for symbol in (construct_type, _construct_field, BaseModel.construct)
    })
    return result
