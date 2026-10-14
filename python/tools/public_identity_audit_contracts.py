"""Actual public identity and deployment-audit value observations.

Process-specific identity values are projected to explicit relationships. The
Source build algorithm is exercised against real package bytes and a private
corpus; native build identities must retain their own implementation identity.
"""

from __future__ import annotations

import asyncio
from dataclasses import asdict
import hashlib
import importlib
import json
import math
import os
from pathlib import Path
import re
from unittest.mock import patch


def public_identity_audit_contracts(scratch: Path) -> dict:
    import mini_loop as public
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager

    identity_module = importlib.import_module("mini_loop.identity")
    audit_module = importlib.import_module("mini_loop.audit")
    package = Path(identity_module.__file__).parent
    digest = hashlib.sha256()
    for path in sorted(package.rglob("*.py")):
        if "__pycache__" not in path.parts:
            digest.update(path.relative_to(package).as_posix().encode())
            digest.update(path.read_bytes())
    build = public.build_id()
    original_cache = identity_module.build_id.cache_info()
    corpus = scratch / "source-build-corpus"
    (corpus / "nested").mkdir(parents=True)
    (corpus / "__pycache__").mkdir()
    (corpus / "b.py").write_bytes(b"B = 2\n")
    (corpus / "nested" / "a.py").write_bytes(b"A = 1\n")
    (corpus / "__pycache__" / "ignored.py").write_bytes(b"ignored = 1\n")
    (corpus / "not-python.txt").write_bytes(b"ignored\n")
    try:
        with patch.object(identity_module, "_PACKAGE", corpus):
            public.build_id.cache_clear()
            first = public.build_id()
            (corpus / "b.py").write_bytes(b"B = 3\n")
            cached = public.build_id()
            public.build_id.cache_clear()
            changed = public.build_id()
            (corpus / "__pycache__" / "ignored.py").write_bytes(b"ignored = 2\n")
            (corpus / "not-python.txt").write_bytes(b"different ignored\n")
            public.build_id.cache_clear()
            ignored = public.build_id()
            # lru_cache retains successful return values, never exceptions.
            # A directory whose name ends in .py exercises the actual read
            # failure without depending on host permissions or a fake hash.
            (corpus / "b.py").unlink()
            (corpus / "b.py").mkdir()
            public.build_id.cache_clear()
            try:
                public.build_id()
            except Exception as error:
                failure_class = type(error).__name__
            else:
                raise AssertionError("Source build_id accepted a source read failure")
            failure_cache_size = public.build_id.cache_info().currsize
            (corpus / "b.py").rmdir()
            (corpus / "b.py").write_bytes(b"B = 4\n")
            recovered = public.build_id()
            (corpus / "b.py").write_bytes(b"B = 5\n")
            cached_after_recovery = public.build_id()
            recovery_cache = public.build_id.cache_info()
    finally:
        public.build_id.cache_clear()
        restored = public.build_id()
    build_observation = dict(
        lowercase_hex=bool(re.fullmatch(r"[0-9a-f]{12}", build)),
        length=len(build), matches_package_content=build == digest.hexdigest()[:12],
        stable_within_process=build == restored == public.build_id(),
        corpus_cached_after_edit=first == cached,
        corpus_changed_after_cache_clear=first != changed,
        corpus_ignores_non_source=changed == ignored,
        cache_maxsize=original_cache.maxsize,
    )
    build_failure = dict(exception_class=failure_class,
                         failure_cache_size=failure_cache_size,
                         recovered_after_repair=bool(re.fullmatch(r"[0-9a-f]{12}", recovered)),
                         success_cached_after_edit=recovered == cached_after_recovery,
                         recovery_cache_misses=recovery_cache.misses,
                         recovery_cache_hits=recovery_cache.hits,
                         recovery_cache_size=recovery_cache.currsize)

    def identity_observation(name, manager=None, auth=None):
        before = public.runtime_identity(manager, auth)
        after = public.runtime_identity(manager, auth)
        return dict(name=name, keys=sorted(before),
                    build_matches_public=before["build"] == public.build_id(),
                    pid_matches_process=before["pid"] == os.getpid(),
                    started_at_stable=before["started_at"] == after["started_at"] == identity_module.STARTED_AT,
                    uptime_nonnegative=before["uptime_s"] >= 0,
                    uptime_nondecreasing=after["uptime_s"] >= before["uptime_s"],
                    posture=before.get("posture"))

    runtime = [identity_observation("without-manager")]
    wall_clock = []
    for name, started_at, now in [
        ("backward", 1000.0, 999.75),
        ("forward", 1000.0, 1000.1254),
        ("forward-after-rounding-boundary", 1000.0, 1000.1265),
        ("backward-near-start", 1000.0, 999.9996),
        ("forward-again", 1000.0, 1001.2345),
        ("positive-tie-even", 0.0, 0.0625),
        ("negative-tie-even", 0.0, -0.0625),
        ("negative-zero", 0.0, -0.0004),
    ]:
        with patch.object(identity_module, "STARTED_AT", started_at), patch.object(identity_module.time, "time", return_value=now):
            observed = public.runtime_identity()
        wall_clock.append(dict(name=name, started_at=started_at, now=now,
                               observed_started_at=observed["started_at"],
                               uptime_s=observed["uptime_s"],
                               uptime_negative_zero=observed["uptime_s"] == 0 and math.copysign(1, observed["uptime_s"]) < 0,
                               build_matches_public=observed["build"] == public.build_id(),
                               pid_matches_process=observed["pid"] == os.getpid()))
    with patch.dict(os.environ, {}, clear=True):
        settings = Settings(fake_llm=True, workspace_root=scratch / "runtime" / "workspace",
                            skills_dir=scratch / "runtime" / "empty-skills",
                            spill_dir=None, trajectory_enabled=False,
                            ast_outline_enabled=False)
        client = FakeAsyncAnthropic()
        manager = SessionManager(settings, client)
        try:
            runtime.append(identity_observation("default-manager", manager))
            auth = public.TokenAuth({"fixture-token": "fixture-owner"})
            runtime.append(identity_observation("authenticated-manager", manager, auth))
            manager_side_effects = dict(sessions=len(manager._sessions), model_calls=client.calls)
        finally:
            asyncio.run(manager.stop())

    def finding(severity, check, detail="fixture detail", remedy="fixture remedy"):
        return dict(severity=severity, check=check, detail=detail, remedy=remedy)

    recipes = [
        dict(name="empty", findings=[]),
        dict(name="caller-order-summary-severity-order", findings=[
            finding("info", "z-info"), finding("critical", "z-critical"),
            finding("medium", "a-medium"), finding("high", "a-high")]),
        dict(name="repeated-severity-counts", findings=[
            finding("medium", "second"), finding("high", "first"),
            finding("medium", "second"), finding("info", "last"),
            finding("high", "first")]),
        dict(name="unicode-newlines-control", findings=[
            finding("high", "检查\nfollow-up", "é e\u0301 用户\n\tsecond\x00", "保留 \n remedy")]),
        dict(name="empty-fields", findings=[finding("critical", "", "", "")]),
        dict(name="single-info", findings=[finding("info", "one")]),
    ]
    rendered = []
    for recipe in recipes:
        findings = [public.Finding(**row) for row in recipe["findings"]]
        before = [asdict(row) for row in findings]
        text = public.render_audit(findings)
        rendered.append(dict(recipe=recipe, findings=before, rendered=text,
                             unchanged_after_render=before == [asdict(row) for row in findings]))
    invalid = []
    for severity in ("", "Critical", "warning", " HIGH ", "中"):
        try:
            public.Finding(**finding(severity, "invalid"))
        except Exception as error:
            invalid.append(dict(severity=severity, exception_class=type(error).__name__, message=str(error)))
        else:
            raise AssertionError("Source accepted an unknown Finding severity")
    result = dict(
        public_aliases={name: getattr(public, name) is value for name, value in {
            "build_id": identity_module.build_id, "runtime_identity": identity_module.runtime_identity,
            "audit": audit_module.audit, "render_audit": audit_module.render,
            "Finding": audit_module.Finding}.items()},
        build=build_observation, build_failure=build_failure,
        runtime=runtime, wall_clock=wall_clock, manager_side_effects=manager_side_effects,
        renderer=rendered, invalid_findings=invalid,
        source_sha256={f"mini_loop/{name}": hashlib.sha256((package / name).read_bytes()).hexdigest()
                       for name in ("__init__.py", "identity.py", "audit.py")},
        producer_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        scope=[
            "Actual root-public exports; only finite string Finding fields and severity values are captured.",
            "Process/build/time values are represented by observed relationships, never equal cross-language fingerprints.",
            "Source build caching, real package-content hash, private source edit and non-source exclusion are executed.",
            "Actual Source read failure leaves an empty cache; repaired input succeeds on retry, then successful content remains cached.",
            "Three actual runtime_identity calls profiles include a real empty manager; model/session side effects are counted.",
            "Eight actual runtime_identity wall-clock profiles retain backward/forward time, negative elapsed values and three-place rounding including signed zero.",
            "Audit alias binds the existing actual manager audit corpus; six direct renderer recipes preserve full text and input order.",
            "Native Finding is a typed value; unknown severity refusal at Render is an explicit boundary adaptation from Source construction.",
            "Python dataclass mutability/slots/equality identity, arbitrary objects, all timestamps and whole-port acceptance are not certified.",
        ],
    )
    json.dumps(result, allow_nan=False)
    return result
