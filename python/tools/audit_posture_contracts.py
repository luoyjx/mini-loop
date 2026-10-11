"""Capture Source remote-posture findings and their human-readable rendering."""

from mini_loop.audit import audit_posture, render
from tools.posture_contracts import posture_contracts


def audit_posture_contracts(scratch) -> dict:
    source = "http://fixture.invalid"
    reports = [(row["name"], row["health"])
               for row in posture_contracts(scratch)["cases"]]
    reports.extend([
        ("missing", {}),
        ("null", {"posture": None}),
        ("empty", {"posture": {}}),
        ("unknown-only", {"posture": {"future": True}}),
        ("explicit-null-wrappers", {"authenticated": True, "posture": {
            "sandbox": None, "secrets": None, "state_store": "NullStateStore"}}),
        ("empty-names", {"authenticated": True, "posture": {
            "sandbox": "", "secrets": "", "sandbox_reason": "",
            "state_store_error": "", "secrets_unregistered": 0}}),
        ("all-selected", {"authenticated": True, "posture": {
            "sandbox": "SeatbeltSandbox", "secrets": "SecretRegistry",
            "secrets_unregistered": 0, "state_store": "SQLiteStateStore",
            "action_journal": "DurableActionJournal"}}),
        ("error-precedence", {"authenticated": True, "posture": {
            "sandbox": "NullSandbox", "sandbox_reason": "fixture backend unavailable",
            "secrets": "NullSecretRegistry", "secrets_unregistered": 9,
            "state_store": "NullStateStore", "state_store_error": "fixture writes failing",
            "action_journal": "InMemoryActionJournal"}}),
    ])
    rows = []
    for name, report in reports:
        findings = audit_posture(report, source=source)
        rows.append(dict(name=name, report=report,
                         findings=[dict(severity=f.severity, check=f.check,
                                        detail=f.detail, remedy=f.remedy) for f in findings],
                         rendered=render(findings)))
    return dict(source=source, cases=rows)
