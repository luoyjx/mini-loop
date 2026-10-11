"""Every module states its runtime-invariant posture -- and the verifier bites.

`tools/verify_invariants.py` is the third verification instrument, after the
mutation guards and the scanning guards. DeepSeek Harness's version of this
convention is explicit about how such conventions rot, and rejects each rot
mode mechanically; this test pins that ours rejects them too, by feeding the
checker functions the exact rot it must refuse:

* a module that declares nothing;
* a NO_RUNTIME_INVARIANT without the exact prefix or with a too-short reason;
* two modules sharing one explanation (the signature of pasted boilerplate);
* a RUNTIME_INVARIANT naming a symbol the module does not define.
"""

import importlib.util
import pathlib
import sys


def _module():
    path = (
        pathlib.Path(__file__).resolve().parent.parent
        / "tools"
        / "verify_invariants.py"
    )
    spec = importlib.util.spec_from_file_location("verify_invariants", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules["verify_invariants"] = module
    spec.loader.exec_module(module)
    return module


def test_the_package_currently_passes():
    assert _module().run() == 0


def _check(tmp_path, name, source) -> list[str]:
    """Run the real verifier against a synthetic one-module package."""

    module = _module()
    package = tmp_path / "mini_loop"
    package.mkdir(exist_ok=True)
    (package / name).write_text(source)
    original_root, original_pkg = module.ROOT, module.PACKAGE
    module.ROOT, module.PACKAGE = tmp_path, package
    problems = []
    try:
        import contextlib
        import io

        buffer = io.StringIO()
        with contextlib.redirect_stdout(buffer):
            code = module.run()
        problems = [
            line.strip()
            for line in buffer.getvalue().splitlines()
            if line.strip().startswith("UNDECLARED")
        ]
        return problems if code else []
    finally:
        module.ROOT, module.PACKAGE = original_root, original_pkg


def test_a_silent_module_is_rejected(tmp_path):
    problems = _check(tmp_path, "quiet.py", "x = 1\n")
    assert problems and "declares neither" in problems[0]


def test_a_wrong_prefix_is_rejected(tmp_path):
    problems = _check(
        tmp_path, "prefixless.py",
        'NO_RUNTIME_INVARIANT = "nothing to check here, honestly, truly, at all"\n',
    )
    assert problems and "must start with" in problems[0]


def test_a_too_short_reason_is_rejected(tmp_path):
    problems = _check(
        tmp_path, "terse.py",
        'NO_RUNTIME_INVARIANT = "No runtime invariant: n/a"\n',
    )
    assert problems and "module-specific reason" in problems[0]


def test_duplicated_boilerplate_is_rejected(tmp_path):
    reason = "No runtime invariant: this text is long enough but it is pasted boilerplate."
    package = tmp_path / "mini_loop"
    package.mkdir()
    (package / "one.py").write_text(f'NO_RUNTIME_INVARIANT = "{reason}"\n')
    problems = _check(tmp_path, "two.py", f'NO_RUNTIME_INVARIANT = "{reason}"\n')
    assert problems and "duplicates" in problems[0]


def test_a_declaration_pointing_at_nothing_is_rejected(tmp_path):
    problems = _check(
        tmp_path, "liar.py",
        'RUNTIME_INVARIANT = "enforced by check_everything: totally covered"\n',
    )
    assert problems and "does not exist" in problems[0]
