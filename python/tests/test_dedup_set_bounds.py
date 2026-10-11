"""The reconstruction dedup sets are bounded (round 224).

Rounds 197/198/208 log each distinct catalog / system-prompt / capability
fingerprint once per process, remembered in a set so it is not re-logged
every round. A long-lived session that keeps minting new fingerprints
(MCP connect/drop, skill loads, permission flips) would grow those sets
without bound -- "bounded output is not bounded work". The cap clears the
set, which costs at most one re-logged spare copy per still-active
fingerprint, never a gap.
"""

from mini_loop.agent import MAX_LOGGED_FINGERPRINTS, _remember_bounded


def test_the_set_never_exceeds_the_cap():
    seen: set[str] = set()
    for i in range(MAX_LOGGED_FINGERPRINTS * 3):
        _remember_bounded(seen, f"fp-{i}")
        assert len(seen) <= MAX_LOGGED_FINGERPRINTS
    # The most recent fingerprint is always remembered (so the current
    # request is not needlessly re-logged).
    assert f"fp-{MAX_LOGGED_FINGERPRINTS * 3 - 1}" in seen


def test_a_repeated_fingerprint_is_not_relogged():
    seen: set[str] = set()
    _remember_bounded(seen, "stable")
    _remember_bounded(seen, "stable")
    assert seen == {"stable"}, "a stable fingerprint must dedup, not grow"


def test_clearing_re_logs_rather_than_dropping(tmp_path):
    """After a clear, a still-active fingerprint is treated as new -- it
    re-logs (a spare copy), which is the safe direction, not a silent gap."""
    seen: set[str] = set()
    for i in range(MAX_LOGGED_FINGERPRINTS):
        _remember_bounded(seen, f"fp-{i}")
    assert len(seen) == MAX_LOGGED_FINGERPRINTS
    # The next distinct fingerprint triggers the clear.
    _remember_bounded(seen, "overflow")
    assert seen == {"overflow"}
    # An earlier fingerprint is now absent -> its next request re-logs it.
    assert "fp-0" not in seen
