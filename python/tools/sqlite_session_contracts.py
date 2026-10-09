"""Actual Source metadata bytes, fallback reads and deletion rollback."""


def sqlite_session_contracts(scratch) -> dict:
    from dataclasses import asdict, replace
    from pathlib import Path
    from mini_loop.storage import SQLiteStateStore, SessionRecord

    root = Path(scratch) / "sqlite-sessions"
    root.mkdir(parents=True, exist_ok=True)
    path = root / "state.db"
    left, right = SQLiteStateStore(path), SQLiteStateStore(path)
    text = '汉字😀 <tag>: comma, quote" slash\\ newline\n separator\u2028'
    record = SessionRecord("saved", "/kept", "", 10.0, 0, "idle", 999,
        todos=({"content": text, "status": "pending", "activeForm": "doing"},),
        owner="", pending_steering=(text,), workspace_bound=True)
    left.upsert_session(record)
    raw = left._db.execute("SELECT todos, pending_steering FROM sessions").fetchone()
    result = dict(input=asdict(record), raw_todos=raw[0], raw_steering=raw[1],
                  initial=[asdict(row) for row in right.load_sessions()])
    left.upsert_session(replace(record, workspace="/updated", system=None,
                              created_at=999.0, run_count=4, status="running"))
    result["updated"] = [asdict(row) for row in right.load_sessions()]
    left.close(); right.close()
    left = SQLiteStateStore(path)
    result["reopened"] = [asdict(row) for row in left.load_sessions()]
    left._db.execute("UPDATE sessions SET todos='', pending_steering='', workspace_bound='false'")
    result["empty_metadata"] = [asdict(row) for row in left.load_sessions()]
    left._db.execute("ALTER TABLE sessions DROP COLUMN owner")
    left._db.execute("ALTER TABLE sessions DROP COLUMN pending_steering")
    left._db.execute("ALTER TABLE sessions DROP COLUMN workspace_bound")
    result["absent_optional"] = [asdict(row) for row in left.load_sessions()]
    left.close()

    deletion = SQLiteStateStore(root / "delete.db")
    deletion.upsert_session(record)
    deletion.append_messages("saved", [{"role": "user", "content": "keep until deletion"}])
    deletion.append_event("saved", {"type": "status", "seq": 1, "status": "idle"})
    deletion.write_action(dict(action_id="action", session_id="saved", message_id="m",
        tool_use_id="u", tool_name="bash", input_hash="hash", status="started",
        result=None, workflow_run_id=None, created_at=1.0, completed_at=None))
    deletion.write_approval(dict(approval_id="approval", session_id="saved", tool_use_id="u",
        tool_name="bash", rule="exec", message="allow?", input_preview="echo safe",
        status="pending", created_at=1.0, resolved_at=None))
    tables = ("sessions", "messages", "events", "actions", "approvals")
    def counts():
        return {table: deletion._db.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
                for table in tables}
    result["before_delete"] = counts()
    deletion._db.execute("CREATE TRIGGER reject_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'reject deletion'); END")
    try:
        deletion.delete_session("saved")
    except Exception as error:
        result["delete_error"] = str(error)
    else:
        raise AssertionError("Source deletion unexpectedly bypassed trigger")
    result["failed_delete"] = counts()
    deletion._db.execute("DROP TRIGGER reject_delete")
    deletion.delete_session("saved")
    result["deleted"] = counts()
    deletion.close()
    return result
