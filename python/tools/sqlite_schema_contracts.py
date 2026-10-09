"""Capture actual SQLiteStateStore opening and additive migration effects."""


def sqlite_schema_contracts(scratch) -> dict:
    import sqlite3
    from pathlib import Path
    from mini_loop.storage import SQLiteStateStore, _SCHEMA

    root = Path(scratch) / "sqlite-schema"
    root.mkdir(parents=True, exist_ok=True)
    tables = ("sessions", "messages", "actions", "events", "approvals")
    recipes = []
    for version in range(8):
        omitted = set()
        if version < 7:
            omitted.update(("owner", "kind", "answer", "pending_steering", "workspace_bound"))
        if version < 5:
            omitted.update(("lease_owner", "lease_until"))
        if version < 3:
            omitted.add("todos")
        if version < 2:
            omitted.add("epoch")
        recipes.append((f"v{version}", version, omitted, []))
    recipes.extend([
        ("v7-missing-binding", 7, {"workspace_bound", "pending_steering"}, []),
        ("v6-missing-lease", 6, {"lease_owner", "lease_until"}, []),
        ("v3-missing-todos", 3, {"todos"}, []),
        ("v2-missing-epoch", 2, {"epoch"}, []),
        ("multiple-version-rows", 1, {"epoch", "todos", "owner"}, [8]),
    ])
    rows = []
    seeds = [("empty", ""), ("future", "CREATE TABLE schema_version (version INTEGER NOT NULL); INSERT INTO schema_version VALUES (8);")]
    for name, version, omitted, extra in recipes:
        lines = [line for line in _SCHEMA.splitlines()
                 if not line.strip() or line.strip().split()[0] not in omitted]
        seed = "\n".join(lines)
        # Removing the final column must also remove the preceding comma.
        seed = seed.replace(",\n);", "\n);")
        seed += f"INSERT INTO schema_version VALUES ({version});"
        for additional in extra:
            seed += f"INSERT INTO schema_version VALUES ({additional});"
        seed += "INSERT INTO sessions (session_id, workspace, system, created_at, run_count, status) VALUES ('saved', '/kept', 'prompt', 12.5, 3, 'running');"
        seeds.append((name, seed))
    for name, literal in (("fractional-current", "7.9"),
                          ("fractional-old", "1.9"),
                          ("negative-huge-real", "-1e100"),
                          ("unicode-version", "'٧'"),
                          ("blob-version", "X'37'"),
                          ("underscore-future", "'7_0'"),
                          ("huge-text-future", "'1_" + "0" * 100 + "'")):
        seeds.append((name, _SCHEMA + f"INSERT INTO schema_version VALUES ({literal});"))
    for name, seed in seeds:
        path = root / (name + ".db")
        db = sqlite3.connect(path)
        db.executescript(seed)
        db.close()
        error = ""
        try:
            store = SQLiteStateStore(path)
        except Exception as exc:
            error = str(exc).replace(str(path), "$DB")
        else:
            store.close()
        db = sqlite3.connect(path)
        columns = {table: [row[1] for row in db.execute(f"PRAGMA table_info({table})")]
                   for table in tables}
        indexes = [row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_autoindex%' ORDER BY name")]
        versions = [row[0] for row in db.execute("SELECT quote(version) FROM schema_version")]
        saved = []
        if "session_id" in columns["sessions"]:
            selected = ", ".join("quote(" + column + ")" for column in columns["sessions"])
            row = db.execute(f"SELECT {selected} FROM sessions WHERE session_id='saved'").fetchone()
            saved = list(row) if row else []
        db.close()
        rows.append(dict(name=name, seed_sql=seed, error=error, columns=columns,
                         indexes=indexes, versions=versions, saved=saved))
    return dict(rows=rows)
