"""Capture actual Source configuration auditing against real filesystem states."""

from pathlib import Path

from mini_loop.audit import audit_settings, render
from mini_loop.config import Settings


def audit_settings_contracts(scratch: Path) -> dict:
    scratch.mkdir(parents=True, exist_ok=True)
    target = scratch / "target"
    target.mkdir()
    target.chmod(0o777)
    cases = [
        ("default", "missing", {}, False, True, True),
        ("localhost", "missing", {"HOST": "localhost"}, False, False, False),
        ("ipv6", "missing", {"HOST": "::1"}, False, False, False),
        ("empty-host", "missing", {"HOST": ""}, False, False, False),
        ("public", "missing", {"HOST": "0.0.0.0"}, False, False, False),
        ("other-loopback", "missing", {"HOST": "127.0.0.2"}, False, False, False),
        ("bracketed-ipv6", "missing", {"HOST": "[::1]"}, False, False, False),
        ("single-token", "missing", {"HOST": "0.0.0.0", "MINILOOP_API_TOKEN": " fixture-token "}, False, False, False),
        ("multiple-tokens", "missing", {"HOST": "0.0.0.0", "MINILOOP_API_TOKENS": "alice:one,bob:two"}, False, False, False),
        ("blank-token", "missing", {"MINILOOP_API_TOKEN": " "}, False, False, False),
        ("comma-only", "missing", {"MINILOOP_API_TOKENS": ", ,"}, False, False, False),
        ("invalid-token", "missing", {"MINILOOP_API_TOKENS": "alice"}, False, False, False),
        ("empty-principal", "missing", {"MINILOOP_API_TOKENS": ":token"}, False, False, False),
        ("shared-token", "missing", {"MINILOOP_API_TOKENS": "alice:one,bob:one"}, False, False, False),
        ("fake-model", "missing", {}, True, False, False),
        ("content-off", "missing", {}, False, True, False),
        ("recording-off", "missing", {}, False, False, True),
        ("writable-order", "writable", {}, True, True, True),
        ("protected", "protected", {}, False, False, False),
        ("writable-file", "file", {}, False, False, False),
        ("symlink-target", "linked", {}, False, False, False),
        ("symlink-cycle", "cycle", {}, False, False, False),
        ("dangling-symlink", "broken", {}, False, False, False),
        ("nondirectory-parent", "file/child", {}, False, False, False),
    ]
    rows = []
    for name, relative, env, fake, trajectory, content in cases:
        relative_root = f"{name}/{relative}"
        root = scratch / relative_root
        settings = Settings(workspace_root=root, fake_llm=fake,
                            trajectory_enabled=trajectory, trajectory_capture_content=content)
        # Settings creates its root. Mutate the filesystem afterward to exercise
        # the audit's real observation of missing/replaced/rebound roots.
        if relative == "missing":
            root.rmdir()
        elif relative in ("writable", "protected"):
            root.chmod(0o777 if relative == "writable" else 0o775)
        elif relative == "file":
            root.rmdir()
            root.write_text("fixture")
            root.chmod(0o666)
        elif relative in ("linked", "broken"):
            root.rmdir()
            root.symlink_to(target if relative == "linked" else scratch / "absent")
        elif relative == "cycle":
            root.rmdir()
            root.symlink_to(root)
        elif relative == "file/child":
            root.rmdir()
            root.parent.rmdir()
            root.parent.write_text("fixture")
        row = dict(name=name, root=relative_root, state=relative, environment=env, fake_llm=fake,
                   trajectory_enabled=trajectory, trajectory_capture_content=content)
        try:
            findings = audit_settings(settings, environ=env)
            row.update(findings=[dict(severity=f.severity, check=f.check,
                                      detail=f.detail.replace(str(scratch), "ROOT"),
                                      remedy=f.remedy.replace(str(scratch), "ROOT")) for f in findings],
                       rendered=render(findings).replace(str(scratch), "ROOT"), error=None)
        except ValueError as exc:
            row.update(findings=None, rendered=None, error=str(exc))
        rows.append(row)
    return dict(cases=rows)
