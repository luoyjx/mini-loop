"""Teams mailbox census: message loss must be visible to BOTH ends.

The probe sent 150 messages and read 100: the drop-oldest bound worked,
the problem ledger recorded it -- and the recipient learned nothing. To
the receiving agent, "150 sent, 100 delivered" was byte-identical to
"100 sent": a coordination failure where neither teammate knows a
message vanished. Round 62's silent-truncation lesson, mailbox edition.

Fixed and pinned here (docs/RSI_RESEARCH_AND_PLAN.md §5): an overflowed
delivery leads with a synthetic notice FROM the mailbox itself, and the
notice spends the delivery bound (99 + notice = MAX_INBOX) rather than
stretching it. Benign behaviors pinned while the census was here:
oversized sends refuse loudly with the limit named, malformed keys
refuse on send and are ledgered on read (a typo must not read as an
eternally empty inbox), and empty content is legal.
"""

from mini_loop.teams import MessageBus, team_key


def _k(name):
    return team_key("t1", name)


def test_an_overflowed_delivery_tells_the_recipient(tmp_path):
    bus = MessageBus(tmp_path / "teams")
    for index in range(150):
        bus.send(_k("a"), _k("b"), f"m{index:03d}")

    delivered = bus.read(_k("b"))
    assert len(delivered) == MessageBus.MAX_INBOX
    notice = delivered[0]
    assert notice["from"] == "mailbox" and notice["type"] == "notice"
    assert "51 older messages were dropped unread" in notice["content"]
    assert delivered[1]["content"] == "m051", "drop-oldest, newest kept"
    assert delivered[-1]["content"] == "m149"
    assert any("dropped" in problem for problem in bus.problems), (
        "the operator's ledger entry must survive the recipient notice"
    )


def test_the_in_memory_path_carries_the_same_notice():
    bus = MessageBus()  # root=None
    bus.inboxes["b"] = [
        {"from": "s", "to": "b", "content": f"m{i}"} for i in range(500)
    ]
    delivered = bus.read("b")
    assert len(delivered) == MessageBus.MAX_INBOX
    assert delivered[0]["type"] == "notice"
    assert "401 older messages were dropped unread" in delivered[0]["content"]
    assert delivered[-1]["content"] == "m499"


def test_a_bounded_delivery_carries_no_notice(tmp_path):
    """A notice on every batch would be noise; only loss speaks."""

    bus = MessageBus(tmp_path / "teams")
    for index in range(3):
        bus.send(_k("a"), _k("b"), f"m{index}")
    delivered = bus.read(_k("b"))
    assert len(delivered) == 3
    assert all(m.get("type") != "notice" for m in delivered)


def test_send_edges_refuse_loudly_and_empty_content_is_legal(tmp_path):
    bus = MessageBus(tmp_path / "teams")
    huge = bus.send(_k("a"), _k("b"), "X" * (MessageBus.MAX_CONTENT + 1))
    assert huge.startswith("Error") and "16,000" in huge

    bad_key = bus.send(_k("a"), "../../etc/passwd", "x")
    assert bad_key.startswith("Error")

    assert bus.read("../../etc/passwd") == []
    assert any("refused" in problem for problem in bus.problems), (
        "a typo'd mailbox must be ledgered, not read as eternally empty"
    )

    assert bus.send(_k("a"), _k("b"), "") == "Sent message to b"
    (row,) = bus.read(_k("b"))
    assert row["content"] == ""
