"""Actual SDK text-integer usage normalization, including exact decimal tails."""
from tools.provider_usage_contracts import provider_usage_contracts


def provider_usage_text_contracts() -> dict:
    values = [
        ("integer", "13"), ("plus-leading-zero", "+0013"),
        ("negative", "-13"), ("ascii-space", "\v13\f"),
        ("nbsp", "\u00a013\u00a0"), ("em-space", "\u200313\u2003"),
        ("negative-zero", "-0"), ("decimal-zero", "13.000"),
        ("negative-decimal", "-13.0"), ("plus-decimal", "+13.00"),
        ("past-float-precision", "9007199254740993.0"),
        ("native-max", "9223372036854775807.0"),
        ("native-min", "-9223372036854775808.0"),
        ("missing-fraction", "13."), ("missing-integer", ".0"),
        ("tiny-fraction", "13.0000000000000000000001"),
        ("negative-tiny-fraction", "-13.0000000000000000000001"),
        ("fraction", "13.75"), ("exponent", "13e0"),
        ("underscore", "1_3"), ("arabic-digits", "١٣"),
        ("fullwidth-digits", "１３"), ("empty", ""),
        ("sign-only", "+"), ("double-point", "13.0.0"),
        ("underscore-decimal", "1_3.0"),
        ("leading-underscore", "_13"), ("trailing-underscore", "13_"),
        ("double-underscore", "1__3"), ("sign-underscore", "+_13"),
        ("underscore-before-point", "13_.0"),
        ("underscore-after-point", "13._0"),
        ("fraction-underscore", "13.0_0"),
    ]
    profiles = [{"name": name, "stream": True,
                 "usage": {"input_tokens": value, "output_tokens": 5}}
                for name, value in values]
    for name, field, value in [
        ("cache-input-tiny-fraction", "cache_creation_input_tokens", "13.0000000000000000000001"),
        ("cache-read-large-decimal", "cache_read_input_tokens", "9007199254740993.0"),
        ("nested-cache-trailing-point", "cache_creation", {"ephemeral_1h_input_tokens": "13."}),
        ("thinking-large-decimal", "output_tokens_details", {"thinking_tokens": "9007199254740993.0"}),
        ("server-fetch-tiny-fraction", "server_tool_use", {"web_fetch_requests": "13.0000000000000000000001"}),
    ]:
        profiles.append({"name": name, "stream": True,
                         "usage": {"input_tokens": 13, "output_tokens": 5, field: value}})
    return provider_usage_contracts(profiles=profiles, capture_consumers=True)
