"""Freeze real python-dotenv parsing, interpolation and environment application."""

import importlib.metadata
import io
import logging
import os
from unittest.mock import patch

from dotenv import load_dotenv
from dotenv.main import DotEnv
from dotenv.parser import parse_stream


def dotenv_contracts() -> dict:
    recipes = [
        ("basic", "A=one\nB=\nC\nexport D=two\n", {}),
        ("comments", "# heading\nA=one#literal\nB=two # comment\nC=' # quoted '\n", {}),
        ("keys", "export A = value\nexported=x\n' spaced = key '=quoted\n", {}),
        ("quotes", "A='it\\'s \\\\ fine'\nB=\"line\\nnext\\t\\r\\a\\b\\f\\v\\\"\\\\\"\n", {}),
        ("unknown-escapes", "A='\\n\\q'\nB=\"\\q\\u0041\\x41\"\n", {}),
        ("multiline", "A='first\nsecond'\nB=\"third\nfourth\"\n", {}),
        ("crlf", "\r\nA=one\r\nB=two\rC=three\n", {}),
        ("unicode", "\u2003A\u00a0=\u2003hi\u2003# end\n'键 名'=你好\n", {}),
        ("recovery", "bad key=value\nA=valid\nB='unterminated\nC=lost\nD=end\n", {}),
        ("trailing", "A='ok' garbage\nB=valid\nC=one;two\n=empty-key\n", {}),
        ("bom", "\ufeffA=one\nB=two\n", {}),
        ("duplicates", "A=first\nB=middle\nA=last\nC\nA\n", {}),
        ("interpolation", "A=${EXT}\nB=${ABSENT}\nC=${ABSENT:-fallback}\nD=$EXT\n", {"EXT": "external"}),
        ("priority", "A=file\nB=${A}\nA=${A}-again\nC=${B}\n", {"A": "external"}),
        ("empty-default", "A=\nB=${A:-fallback}\nC\nD=${C:-fallback}\nE=${ABSENT:-}\n", {}),
        ("variable-syntax", "A=${A:-first}\nB=${键:-你好}\nC=${A:-${OTHER}}\nD=${A-default}\nE=${}\n", {}),
        ("blank", "\n # only comments\n", {"KEEP": "original"}),
        ("valueless", "A\nB\n", {}),
        ("disabled", "A=file\n", {"PYTHON_DOTENV_DISABLED": "true", "A": "external"}),
    ]
    rows = []
    # Source warns for malformed lines. Capture logging separately from data;
    # parser bindings below preserve exact error flags and original line spans.
    logger = logging.getLogger("dotenv.main")
    for name, text, env in recipes:
        bindings = [dict(key=b.key, value=b.value, error=b.error,
                         original=b.original.string, line=b.original.line)
                    for b in parse_stream(io.StringIO(text))]
        for override in (True, False):
            with patch.dict(os.environ, env, clear=True), patch.object(logger, "warning"):
                values = DotEnv(dotenv_path=None, stream=io.StringIO(text), override=override).dict()
                loaded, fault = None, None
                try:
                    loaded = load_dotenv(stream=io.StringIO(text), override=override)
                except Exception as exc:
                    fault = dict(type=type(exc).__name__, message=str(exc))
                applied = dict(os.environ)
            rows.append(dict(name=name, text=text, environment=env, override=override,
                             bindings=bindings, values=[dict(key=k, value=v) for k, v in values.items()],
                             loaded=loaded, fault=fault, applied=applied))
    return dict(version=importlib.metadata.version("python-dotenv"), cases=rows)
