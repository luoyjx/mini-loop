"""Capture the actual ast.parse grammar and error profile, without executing code."""

import ast
import base64
import sys


def python_syntax_contracts() -> dict:
    # ast.parse accepts several constructs that compiling/executing would reject.
    # Keep those cases: a compiler or approximate grammar is not this contract.
    samples = [
        ("empty", ""),
        ("comment", "# coding: utf-8\n"),
        ("assignment", "answer = 42\n"),
        ("unicode-identifier", "变量 = '✓'\n"),
        ("unicode-string", "value = '😀'\n"),
        ("return-at-module", "return 1\n"),
        ("break-at-module", "break\n"),
        ("continue-at-module", "continue\n"),
        ("await-at-module", "await call()\n"),
        ("nonlocal-at-module", "nonlocal value\n"),
        ("duplicate-arguments", "def f(x, x):\n    pass\n"),
        ("async-function", "async def f():\n    return await g()\n"),
        ("decorator", "@decorate(1)\ndef f():\n    pass\n"),
        ("positional-only", "def f(x, /, y=1, *, z=2):\n    return x\n"),
        ("type-union", "def f(x: int | str) -> list[int]:\n    return []\n"),
        ("walrus", "if (value := call()):\n    print(value)\n"),
        ("match", "match value:\n    case {'x': x} if x:\n        pass\n"),
        ("exception-group", "try:\n    f()\nexcept* ValueError:\n    pass\n"),
        ("mixed-except", "try:\n    pass\nexcept ValueError:\n    pass\nexcept* TypeError:\n    pass\n"),
        ("python312-type-statement", "type Alias = int\n"),
        ("python312-type-parameter", "def f[T](x: T):\n    return x\n"),
        ("fstring", "value = f'{name!r:>10}'\n"),
        ("fstring-unclosed", "value = f'{name'\n"),
        ("fstring-empty", "value = f'{}'\n"),
        ("fstring-backslash", "value = f'{chr(92) + \\\"x\\\"}'\n"),
        ("fstring-same-quote", 'value = f"{mapping["key"]}"\n'),
        ("unterminated-string", "value = 'unfinished\n"),
        ("unterminated-triple-string", "value = '''unfinished\n"),
        ("unclosed-parenthesis", "value = (1 +\n"),
        ("mismatched-bracket", "value = [1, 2)\n"),
        ("unexpected-close", "value = 1)\n"),
        ("missing-colon", "if True\n    pass\n"),
        ("missing-indent", "if True:\npass\n"),
        ("unexpected-indent", "    pass\n"),
        ("unindent", "if True:\n    pass\n  pass\n"),
        ("mixed-tabs-spaces", "if True:\n\tpass\n        pass\n"),
        ("dangling-decorator", "@decorate\n"),
        ("dangling-else", "else:\n    pass\n"),
        ("assignment-target", "1 = value\n"),
        ("delete-target", "del 1\n"),
        ("augmented-target", "a, b += values\n"),
        ("keyword-argument-order", "f(x=1, 2)\n"),
        ("duplicate-keyword", "f(x=1, x=2)\n"),
        ("argument-after-kwargs", "f(**values, *items)\n"),
        ("leading-zero", "value = 0123\n"),
        ("numeric-underscore", "value = 1__2\n"),
        ("invalid-hex", "value = 0xG\n"),
        ("invalid-exponent", "value = 1e+\n"),
        ("bytes-nonascii", "value = b'é'\n"),
        ("null-character", "value = 1\x00\n"),
        ("bom-in-text", "\ufeffvalue = 1\n"),
        ("replacement-character", "value = \ufffd\n"),
        ("lone-surrogate", "value = '\ud800'\n"),
        ("nonbreaking-space", "value\u00a0= 1\n"),
        ("line-continuation", "value = 1 + \\\n  2\n"),
        ("line-continuation-garbage", "value = 1 \\ x\n"),
        ("crlf", "if True:\r\n    pass\r\n"),
        ("error-line-two", "value = 1\nif True\n    pass\n"),
        ("multiline-error", "value = (\n    1 +\n)\n"),
        ("future-import-late", "value = 1\nfrom __future__ import annotations\n"),
        ("unknown-future", "from __future__ import imaginary_feature\n"),
        ("invalid-unicode-escape", "value = '\\uXYZW'\n"),
    ]
    rows = []
    for name, source in samples:
        row = dict(name=name, source_repr=ascii(source),
                   source_utf8=base64.b64encode(source.encode("utf-8", "surrogatepass")).decode("ascii"),
                   kind="clean", line=0, message="", error_class="")
        try:
            ast.parse(source)
        except SyntaxError as error:
            row.update(kind="syntax", line=int(error.lineno or 0),
                       message=error.msg or "syntax error", error_class=type(error).__name__)
        except Exception as error:
            row.update(kind="error", message=str(error), error_class=type(error).__name__)
        rows.append(row)
    return dict(implementation=sys.implementation.name,
                version=list(sys.version_info[:3]), operation="ast.parse(source)",
                encoding="UTF-8 with surrogatepass, base64", cases=rows)
