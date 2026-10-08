"""Exercise the real masked RAM store with controlled clock and entropy boundaries."""
import base64
from dataclasses import asdict, replace
from pathlib import Path
import tempfile
from unittest.mock import patch


def efficiency_store_contracts() -> dict:
    from mini_loop.token_efficiency import MaskedRawArtifactStore
    base = dict(directory=".token-efficiency/raw", ttl_seconds=10,
                max_artifact_bytes=8, max_total_bytes=12)
    constructors = []
    specs = [("default", {}), ("zero-ttl", dict(ttl_seconds=0)),
             ("negative-ttl", dict(ttl_seconds=-1)),
             ("zero-item", dict(max_artifact_bytes=0)),
             ("negative-total", dict(max_total_bytes=-1)),
             ("item-over-total", dict(max_artifact_bytes=13)),
             ("large-limits", dict(max_artifact_bytes=10**100, max_total_bytes=10**101)),
             ("absolute", dict(directory="/tmp/raw")),
             ("parent", dict(directory="a/../b")),
             ("empty-directory", dict(directory="")), ("dot", dict(directory=".")),
             ("normalized-directory", dict(directory="a/./b"))]
    recipes = [
        ("roundtrip", [("put", "é🔒"), ("get", "0"), ("get-other", "0")]),
        ("capacity", [("put", "12345678"), ("put", "5678"), ("put", "x"), ("discard", "0"), ("put", "x")]),
        ("oversize", [("put", "123456789")]),
        ("empty", [("put", ""), ("get", "0")]),
        ("expiry", [("put", "secret"), ("clock", "10"), ("get", "0"), ("get", "0")]),
        ("sweep", [("put", "a"), ("clock", "5"), ("put", "b"), ("clock", "10"), ("sweep", ""), ("get", "1"), ("clock", "15"), ("sweep", "")]),
        ("put-sweeps", [("put", "12345678"), ("clock", "10"), ("put", "12345678"), ("get", "0")]),
        ("lifecycle", [("put", "a"), ("discard", "0"), ("discard", "0"), ("put", "b"), ("clear", ""), ("put", "c"), ("close", ""), ("close", ""), ("clear", ""), ("get", "2"), ("put", "a"), ("sweep", ""), ("discard", "2")]),
        ("closed-precedence", [("close", ""), ("put", "123456789"), ("get-invalid", "../bad"), ("discard-invalid", "raw_bad"), ("clock", "fault"), ("sweep", "")]),
        ("clock-faults", [("clock", "fault"), ("get-invalid", "bad"), ("get-missing", ""), ("put", "a"), ("sweep", ""), ("clock", "nan"), ("put", "a"), ("clock", "inf"), ("sweep", "")]),
        ("backward-clock", [("clock", "5"), ("put", "a"), ("clock", "-1"), ("get", "0"), ("clock", "15"), ("get", "0")]),
        ("integrity-content", [("put", "abcd"), ("corrupt", "payload"), ("get", "0")]),
        ("integrity-size", [("put", "abcd"), ("corrupt", "size"), ("get", "0")]),
        ("read-limit", [("put", "abcd"), ("corrupt", "oversize"), ("get", "0")]),
        ("collision", [("entropy", "constant"), ("put", "a"), ("put", "b")]),
    ]
    with tempfile.TemporaryDirectory(prefix="efficiency-store-") as tmp:
        workspace = Path(tmp)
        for name, changes in specs:
            config = base | changes
            try:
                store = MaskedRawArtifactStore(workspace, **config)
                output, error = dict(closed=store.closed), None
                store.close()
            except Exception as fault:
                output, error = None, dict(kind=type(fault).__name__, message=str(fault))
            constructors.append(dict(name=name, input=config, output=output, error=error))
        rows = []
        for name, recipe in recipes:
            now, mode, counter, constant = 0.0, "normal", 0, False
            def clock():
                if mode == "fault":
                    raise RuntimeError("clock offline")
                return float(mode) if mode in ("nan", "inf") else now
            def token(n):
                nonlocal counter
                assert n == 32
                counter += 1
                return base64.urlsafe_b64encode(bytes([1 if constant else counter]) * n).decode().rstrip("=")
            store = MaskedRawArtifactStore(workspace, **base, clock=clock)
            other = MaskedRawArtifactStore(workspace, **base, clock=clock)
            refs, steps = [], []
            with patch("mini_loop.token_efficiency.secrets.token_urlsafe", side_effect=token):
                for op, value in recipe:
                    output, error = None, None
                    try:
                        if op == "clock":
                            mode = value if value in ("fault", "nan", "inf") else "normal"
                            if mode == "normal": now = float(value)
                        elif op == "entropy": constant = True
                        elif op == "put":
                            pointer = store.put_masked(value)
                            refs.append(pointer.ref)
                            output = asdict(pointer)
                        elif op == "get": output = store.get_masked(refs[int(value)])
                        elif op == "get-other": output = other.get_masked(refs[int(value)])
                        elif op == "get-invalid": output = store.get_masked(value)
                        elif op == "get-missing": output = store.get_masked("raw_" + "x" * 43)
                        elif op == "discard": output = store.discard(refs[int(value)])
                        elif op == "discard-invalid": output = store.discard(value)
                        elif op in ("close", "clear", "sweep"): output = getattr(store, op)()
                        elif op == "corrupt":
                            ref = refs[0]
                            record = store._records[ref]
                            changes = dict(payload=b"wxyz") if value == "payload" else dict(size_bytes=5) if value == "size" else dict(payload=b"x" * 9)
                            store._records[ref] = replace(record, **changes)
                        else: raise AssertionError(op)
                    except Exception as fault:
                        error = dict(kind=type(fault).__name__, message=str(fault))
                    steps.append(dict(op=op, value=value, output=output, error=error, closed=store.closed))
            rows.append(dict(name=name, config=base, steps=steps))
            store.close(); other.close()
        assert list(workspace.iterdir()) == [], "RAM store wrote workspace content"
    return dict(constructors=constructors, recipes=rows)
