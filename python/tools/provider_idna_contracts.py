"""Finite actual-provider hostname observations at the SDK request boundary.

This captures request objects through the existing MockTransport seam, not DNS,
TCP, TLS or wire-host behavior. The original-host ASCII branch, whole-host
lowering and strict IDNA checks are exercised by the real HTTPX implementation.
"""
from __future__ import annotations

import hashlib
import platform
import unicodedata
from pathlib import Path
from unittest.mock import patch

import anthropic
from anthropic import _base_client as anthropic_base_client
import httpx
from httpx import _urlparse as httpx_urlparse
import idna
from idna import core as idna_core, idnadata, intranges

import mini_loop.providers as providers
import tools.provider_endpoint_contracts as endpoint_contracts
import tools.provider_url_contracts as url_contracts


# Inputs only: outcomes and ASCII encodings are captured, never synthesized.
# ASCII-only controls intentionally reach HTTPX's branch that bypasses IDNA.
IDNA_HOSTS = (
    ("unicode-host", "bücher.invalid", "unicode"),
    ("punycode-host", "xn--bcher-kva.invalid", "ascii"),
    ("ascii-underscore", "_srv.invalid", "ascii-bypass"),
    ("ascii-malformed-alabel", "xn--a.invalid", "ascii-bypass"),
    ("ascii-long-label", "a" * 64 + ".invalid", "ascii-bypass"),
    ("uppercase-unicode-host", "BÜCHER.INVALID", "lower-order"),
    ("uppercase-sharp-s-host", "FAẞ.INVALID", "lower-order"),
    ("dotted-i-host", "İ.invalid", "lower-order"),
    ("greek-across-dot-host", "ΟΣ.invalid", "lower-order"),
    ("lower-becomes-ascii-with-invalid-label", "K.-bad.invalid", "lower-order"),
    ("nfc-composed", "é.invalid", "nfc"),
    ("nfc-decomposed", "e\u0301.invalid", "nfc"),
    ("leading-combining", "\u0301a.invalid", "leading-combining"),
    ("ideographic-dot", "bücher\u3002invalid", "separator"),
    ("fullwidth-dot", "bücher\uff0einvalid", "separator"),
    ("halfwidth-dot", "bücher\uff61invalid", "separator"),
    ("trailing-unicode-dot", "bücher\u3002", "separator"),
    ("empty-unicode-label", "bücher..invalid", "separator"),
    ("label-63-with-unicode", "a" * 63 + ".é.invalid", "length"),
    ("label-64-with-unicode", "a" * 64 + ".é.invalid", "length"),
    ("zwnj-joining-context", "ب\u200cب.invalid", "contextj"),
    ("zwnj-nonjoining-context", "ا\u200cب.invalid", "contextj"),
    ("zwj-virama-context", "क्\u200dष.invalid", "contextj"),
    ("zwj-without-virama", "a\u200db.invalid", "contextj"),
    ("middle-dot-context", "l·l.invalid", "contexto"),
    ("middle-dot-outside-context", "a·a.invalid", "contexto"),
    ("greek-numeral-context", "\u0375α.invalid", "contexto"),
    ("greek-numeral-outside-context", "\u0375a.invalid", "contexto"),
    ("hebrew-punctuation-context", "א\u05f3.invalid", "contexto"),
    ("hebrew-punctuation-outside-context", "a\u05f3.invalid", "contexto"),
    ("katakana-middle-dot-context", "カ・ナ.invalid", "contexto"),
    ("katakana-middle-dot-outside-context", "a・a.invalid", "contexto"),
    ("arabic-indic-digits", "ا١.invalid", "contexto-digits"),
    ("extended-arabic-indic-digits", "ا۱.invalid", "contexto-digits"),
    ("mixed-arabic-digit-sets", "ا١۱.invalid", "contexto-digits"),
    ("numeric-label-before-rtl", "123.א.invalid", "labelwise-bidi"),
    ("rtl-with-european-digit", "א1.invalid", "bidi"),
    ("rtl-mixed-numeral-directions", "א1١.invalid", "bidi"),
    ("rtl-mixed-latin", "אa.invalid", "bidi"),
    ("disallowed-snowman", "☃.invalid", "disallowed"),
    ("unicode-with-ascii-underscore", "bücher._srv.invalid", "ascii-bypass"),
    ("newer-unicode-table-codepoint", "\U00011f04.invalid", "unicode-version"),
)


def provider_idna_contracts() -> dict:
    """Capture a finite hostname set with real provider, SDK and HTTPX code."""
    versions = {
        "sdk_version": anthropic.__version__,
        "httpx_version": httpx.__version__,
        "idna_version": idna.__version__,
        "idna_table_unicode_version": idnadata.__version__,
        "unicode_version": unicodedata.unidata_version,
    }
    expected = {
        "sdk_version": "0.107.1",
        "httpx_version": "0.28.1",
        "idna_version": "3.18",
        "idna_table_unicode_version": "17.0.0",
        "unicode_version": "14.0.0",
    }
    if versions != expected or platform.python_implementation() != "CPython":
        raise RuntimeError(f"hostname contracts require the fixed CPython/SDK profile: {versions}")

    urls = tuple((name, f"https://{host}/tenant") for name, host, _ in IDNA_HOSTS)
    # Reuse its real AnthropicCompatibleProvider.create_client, direct/SSE
    # consumers, environment clearing, exception stage and MockTransport capture.
    # The URL inventory is restored even when capture fails. Export/tests are serial.
    with patch.object(url_contracts, "URLS", urls):
        result = url_contracts.provider_url_contracts()
    profiles = {name: profile for name, _, profile in IDNA_HOSTS}
    for row in result["cases"]:
        row["profile"] = profiles[row["name"]]

    source_paths = {
        "mini_loop/providers.py": Path(providers.__file__),
        "tools/provider_endpoint_contracts.py": Path(endpoint_contracts.__file__),
        "tools/provider_url_contracts.py": Path(url_contracts.__file__),
        "tools/provider_idna_contracts.py": Path(__file__),
        "anthropic/_base_client.py": Path(anthropic_base_client.__file__),
        "httpx/_urlparse.py": Path(httpx_urlparse.__file__),
        "idna/core.py": Path(idna_core.__file__),
        "idna/idnadata.py": Path(idnadata.__file__),
        "idna/intranges.py": Path(intranges.__file__),
    }
    result.update(versions)
    result["python_implementation"] = platform.python_implementation()
    result["python_version"] = platform.python_version()
    result["source_sha256"] = {
        name: hashlib.sha256(path.read_bytes()).hexdigest()
        for name, path in source_paths.items()
    }
    result["scope"] = "Actual provider/SDK direct and SSE request objects via MockTransport; no live network or wire-host claim."
    return result
