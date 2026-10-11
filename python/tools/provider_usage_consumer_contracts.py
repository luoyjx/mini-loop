"""Real SDK scalar usage and actual repository metering observations."""
from __future__ import annotations

from tools.provider_usage_contracts import provider_usage_contracts


def provider_usage_consumer_contracts() -> dict:
    profiles = []
    scalars = [("integer", 13), ("zero", 0), ("negative", -3),
        ("float-integral", 13.0), ("float-fraction", 13.75),
        ("float-negative", -3.75), ("true", True), ("false", False),
        ("numeric-string", "13"), ("fraction-string", "13.75"),
        ("invalid-string", "tokens"), ("null", None),
        ("array", [13]), ("object", {"count": 13})]
    for name, value in scalars:
        profiles.append({"name": "input-" + name,
            "usage": {"input_tokens": value, "output_tokens": 5}, "stream": True})
    profiles += [
        {"name": "required-strings-cache-coerced", "stream": True,
            "usage": {"input_tokens": "13", "output_tokens": "5", "cache_creation_input_tokens": "17"}},
        {"name": "root-fraction-cache-fraction", "stream": True,
            "usage": {"input_tokens": 13.75, "output_tokens": 5.25,
                "cache_read_input_tokens": 2.75, "cache_creation_input_tokens": "17"}},
        {"name": "input-strings-delta-numeric", "modes": ["stream"],
            "usage": {"input_tokens": "13", "output_tokens": 0},
            "delta_usage": {"input_tokens": 21, "output_tokens": 5}},
        {"name": "input-numeric-delta-strings", "modes": ["stream"],
            "usage": {"input_tokens": 13, "output_tokens": 0},
            "delta_usage": {"input_tokens": "21", "output_tokens": "5"}},
    ]
    return provider_usage_contracts(profiles=profiles, capture_consumers=True)
