"""Capture actual Source sandbox construction; no shell commands are executed."""

from pathlib import Path
from unittest.mock import patch

from mini_loop.sandbox import (
    NullSandbox, SandboxUnavailable, SeatbeltSandbox, UnavailableSandbox,
    default_sandbox, default_unreadable_roots, unavailable_reason,
)


def sandbox_contracts(scratch) -> dict:
    root = (Path(scratch) / "sandbox").resolve()
    root.mkdir(parents=True)
    home = root / "home"
    home.mkdir()
    workspace = root / "workspace"
    workspace.mkdir()
    linked = root / "linked"
    linked.mkdir()
    (linked / ".git").write_text("gitdir: ../repo/.git/worktrees/linked\n")
    alternate = root / "alternate"
    alternate.mkdir()
    (alternate / ".git").write_text("gitdir: ../other/.git/worktrees/alternate\n")
    alias = root / "alias"
    alias.symlink_to(workspace, target_is_directory=True)
    command = "printf '%s\\n' 'hello; $(false)'"

    def text(value):
        return value.replace(str(root), "<ROOT>")

    def paths(values):
        return [text(str(value)) for value in values]

    def observe(sandbox):
        result = dict(kind=type(sandbox).__name__, confined=sandbox.confined,
                      describe=text(sandbox.describe),
                      argv=[text(part) for part in sandbox.argv(command)],
                      reason=getattr(sandbox, "reason", None))
        if isinstance(sandbox, SeatbeltSandbox):
            result.update(writable=paths(sandbox.writable_roots),
                          unreadable=paths(sandbox.unreadable_roots),
                          extra=paths(sandbox._extra_writable),
                          allow_network=sandbox.allow_network,
                          preview=text(sandbox.preview(command)))
        return result

    cases = []
    recipes = [
        ("plain", [workspace], [], [], False, workspace),
        ("protected-network", [workspace], [home / ".ssh", home / ".netrc"], [], True, alternate),
        ("duplicates-extra", [workspace, workspace, root / "ordinary"],
         [home / ".ssh", home / ".ssh"], [root / "extra", workspace], False, alternate),
        ("extra-only", [], [], [root / "extra"], False, workspace),
        ("policy-syntax-path", [root / 'quote\" (allow default) ; space'],
         [root / "protected\nnewline"], [], False, alternate),
        ("linked-worktree", [linked], [], [], False, alternate),
        ("symlink-and-missing", [alias / "missing" / ".."], [], [], False, alias),
        ("home-expansion", ["~/project"], ["~/.aws"], [], False, workspace),
    ]
    with patch.object(Path, "home", return_value=home), patch.dict("os.environ", {"HOME": str(home)}):
        for name, writable, unreadable, extra, network, target in recipes:
            sandbox = SeatbeltSandbox(writable_roots=writable, unreadable_roots=unreadable,
                                      _extra_writable=extra, allow_network=network)
            rebound = sandbox.for_workspace(target)
            cases.append(dict(name=name, writable=paths(writable), unreadable=paths(unreadable),
                              extra=paths(extra), allow_network=network, target=text(str(target)),
                              before=observe(sandbox), after=observe(rebound),
                              same_instance=rebound is sandbox))
        credential_roots = paths(default_unreadable_roots())
        with patch("mini_loop.sandbox.platform.system", return_value="Darwin"), patch(
            "mini_loop.sandbox.os.path.exists", return_value=True
        ):
            factory_defaults = observe(default_sandbox(workspace))
        selections = []
        for system, binary in (("Darwin", True), ("Darwin", False), ("Linux", True), ("Windows", False)):
            with patch("mini_loop.sandbox.platform.system", return_value=system), patch(
                "mini_loop.sandbox.os.path.exists", return_value=binary
            ):
                for protect in (True, False):
                    for require in (False, True):
                        row = dict(system=system, binary_exists=binary, protect_credentials=protect,
                                   require=require, available=SeatbeltSandbox.available(),
                                   unavailable_reason=unavailable_reason())
                        try:
                            sandbox = default_sandbox(workspace, unreadable_roots=[root / "operator"],
                                                      allow_network=True, require=require,
                                                      protect_credentials=protect)
                            row.update(result=observe(sandbox), error=None)
                        except SandboxUnavailable as exc:
                            row.update(result=None, error=dict(kind=type(exc).__name__, message=str(exc)))
                        selections.append(row)

    markers = []
    for name, content in (
        ("relative", "gitdir: ../repo/.git/worktrees/unit\n"),
        ("absolute", f"gitdir: {root}/repo/.git/worktrees/unit\n"),
        ("leading-space", " gitdir: ../repo/.git/worktrees/unit\n"),
        ("wrong-parent", "gitdir: ../repo/metadata/worktrees/unit\n"),
        ("later-valid-line", "gitdir: ../wrong/unit\ngitdir: ../repo/.git/worktrees/unit\n"),
        ("unicode-line-break", "ignored\u2028gitdir: ../repo/.git/worktrees/unit\n"),
        ("empty", ""),
    ):
        (workspace / ".git").write_text(content)
        sandbox = SeatbeltSandbox(writable_roots=[workspace])
        markers.append(dict(name=name, content=text(content), writable=paths(sandbox.writable_roots)))
    (workspace / ".git").unlink()
    (workspace / ".git").mkdir()
    markers.append(dict(name="directory", content=None,
                        writable=paths(SeatbeltSandbox(writable_roots=[workspace]).writable_roots)))
    errors = []
    try:
        SeatbeltSandbox(writable_roots=[])
    except ValueError as exc:
        errors.append(dict(name="empty-writable", kind=type(exc).__name__, message=str(exc)))
    (workspace / ".git").rmdir()
    (workspace / ".git").write_bytes(b"\xff")
    try:
        SeatbeltSandbox(writable_roots=[workspace])
    except UnicodeDecodeError as exc:
        errors.append(dict(name="invalid-marker-encoding", kind=type(exc).__name__, message=str(exc)))
    null = NullSandbox()
    unavailable = UnavailableSandbox("fixture unavailable")
    return dict(command=command, cases=cases, selections=selections, markers=markers,
                credential_roots=credential_roots, factory_defaults=factory_defaults, errors=errors,
                null=observe(null), unavailable=observe(unavailable),
                null_rebind_same=null.for_workspace(alternate) is null,
                unavailable_rebind_same=unavailable.for_workspace(alternate) is unavailable)
