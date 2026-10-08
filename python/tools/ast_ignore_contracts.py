"""Observe AST ignore guards and the installed Source GitIgnoreSpec semantics."""

import pathspec
from mini_loop.ast_context import GitIgnoreSpec, _validate_ignore_patterns


def ast_ignore_contracts() -> dict:
    recipes = [
        ("null-lines", ["", "#comment", "/", "  "], ["code.py", "src/code.py"]),
        ("basename", ["*.py"], ["code.py", "src/code.py", "code.ts", ".hidden.py"]),
        ("root-anchor", ["/code.py"], ["code.py", "src/code.py", "code.py/child"]),
        ("multi-segment", ["src/code.py"], ["src/code.py", "nested/src/code.py", "src/code.py/child"]),
        ("directory", ["cache/"], ["cache", "cache/", "cache/code.py", "src/cache/code.py"]),
        ("everything", ["**"], ["a", "a/b", "", "\n"]),
        ("star", ["*"], ["a", "a/b", "", "\n"]),
        ("all-directories", ["**/"], ["a", "a/", "a/b", "a/b/"] ),
        ("star-directory", ["*/"], ["a", "a/", "a/b"]),
        ("collapsed-globstar", ["**/**"], ["a", "a/b", "a/b/c"]),
        ("trailing-globstar", ["src/**"], ["src", "src/", "src/code.py", "src/a/code.py"]),
        ("inner-globstar", ["a/**/b"], ["a/b", "a/x/b", "a/x/y/b", "a/x/c"]),
        ("leading-globstar", ["**/code.py"], ["code.py", "src/code.py", "a/b/code.py"]),
        ("negation", ["*.py", "!keep.py"], ["keep.py", "src/keep.py", "other.py"]),
        ("file-priority", ["!cache/code.py", "cache/"], ["cache/", "cache/code.py", "cache/other.py"]),
        ("directory-priority", ["cache/", "!cache/code.py"], ["cache/", "cache/code.py", "cache/other.py"]),
        ("ordered-negation", ["a", "!a", "a"], ["a", "a/", "a/child"]),
        ("blank-index", ["", "#null", "*.py", "", "!keep.py"], ["keep.py", "other.py", "other.ts"]),
        ("escaped-leading", [r"\#name", r"\!name"], ["#name", "!name", "name"]),
        ("leading-space", [" name"], [" name", "name", "src/ name"]),
        ("trailing-space", ["name "], ["name", "name "]),
        ("escaped-space", ["name\\ "], ["name", "name "]),
        ("escaped-star", [r"a\*"], ["a*", "abc", "src/a*"]),
        ("single-rune", ["?.py"], ["a.py", "😀.py", "ab.py", "src/😀.py"]),
        ("class", ["[ab].py"], ["a.py", "b.py", "c.py"]),
        ("negated-class", ["[!a-c].py"], ["a.py", "c.py", "d.py", "😀.py"]),
        ("caret-class", ["[^a-c].py"], ["a.py", "d.py"]),
        ("literal-brackets", ["[][!].py"], ["].py", "[.py", "!.py", "a.py"]),
        ("literal-hyphen", ["[]-].py"], ["].py", "-.py", "a.py"]),
        ("unclosed-class", ["[bad"], ["[bad", "bad"]),
        ("posix-looking-class", ["[[:digit:]]"], ["1", "1]", "d]", "[]", ":]"]),
        ("range-error", ["[z-a]"], ["z"]),
        ("escape-error", ["name\\"], ["name"]),
        ("negation-error", ["!"], ["a"]),
        ("star-guard", ["*a*a*a*ab"], ["a"]),
        ("globstar-guard", ["**/a/**/b/**"], ["a/b"]),
        ("guard-escaped-stars", [r"a\*\*\*"], ["a***", "abc"]),
        ("comment-stars", ["#****"], ["a"]),
        ("length-guard", ["a" * 4097], ["a"]),
        ("normalization", ["code.py"], ["/code.py", "./code.py", "//code.py", "././code.py"]),
        ("final-newline", ["code.py"], ["code.py\n", "code.py\nx", "code.py\n\n"]),
    ]
    rows = []
    for name, lines, paths in recipes:
        row = {"name": name, "lines": lines, "paths": paths}
        try:
            _validate_ignore_patterns(lines)
            spec = GitIgnoreSpec.from_lines(lines)
            row["pattern_count"] = len(spec.patterns)
            row["matches"] = [{"include": result.include, "index": result.index}
                              for result in (spec.check_file(path) for path in paths)]
        except Exception as error:
            row["error_class"] = type(error).__name__
            row["error"] = str(error)
        rows.append(row)
    return {"pathspec_version": pathspec.__version__, "patterns": rows}
