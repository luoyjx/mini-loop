"""The checklist from round 49, made executable.

Four surfaces in a row failed the same four questions -- skills (45), memory
(46), cron (47), MCP (49) -- and round 49 wrote them down rather than
rediscover them a fifth time:

    1. can one entry silently replace another?
    2. is the content bounded?
    3. does a failure report?
    4. does the value cross into a sink or a prompt?

Writing a checklist down is not applying it. This applies it, once, to every
content store in the package, so surface seven is covered when it arrives
instead of after somebody happens to read it.

Two more surfaces failed on the way in. `teams.MessageBus` delivered a
2,000,000-character message whole into a peer's message stream and 2,000
messages as a single injection, and wrote them to disk unmasked. `TaskStore`
stored a 2,000,000-character description -- work instructions another agent
claims and acts on -- also unmasked. Sinks ten and eleven.
"""

import json
import pathlib

import pytest

from mini_loop.cron import CronScheduler
from mini_loop.memory import MemoryStore
from mini_loop.secrets import SecretRegistry
from mini_loop.skills import SkillLoader
from mini_loop.tasks import TaskStore
from mini_loop.teams import MessageBus

SECRET = "sk-CHECKLIST-CANARY-0123456789ab"
#: A credential that `json.dumps` rewrites: the accented letters escape to
#: `\uXXXX` and the quote/backslash to `\"`/`\\`. A store that masks the
#: *serialized* text instead of the structure searches for the raw bytes and
#: never finds them -- so this value is the one that separates the two orders.
ESCAPING_SECRET = 'clé-secrète-"café"\\Ω-0123456789'
HUGE = "X" * 2_000_000


def _registry(secret=SECRET):
    return SecretRegistry.from_environ(environ={"P_API_KEY": secret})


class NoSessions:
    def get(self, session_id):
        return None


# Each entry: build a store, write oversized content through it, write a
# credential through it, and say where its files live.
def _memory(tmp_path, secret=SECRET):
    store = MemoryStore(tmp_path / "mem", secrets=_registry(secret))
    return store, lambda: store.write("big", "project", "d", HUGE), \
        lambda: store.write("creds", "project", f"k {secret}", f"b {secret}"), \
        tmp_path / "mem"


def _teams(tmp_path, secret=SECRET):
    bus = MessageBus(tmp_path / "teams", secrets=_registry(secret))
    return bus, lambda: bus.send("a", "t/b", HUGE), \
        lambda: bus.send("a", "t/b", f"the key is {secret}"), \
        tmp_path / "teams"


def _tasks(tmp_path, secret=SECRET):
    store = TaskStore(tmp_path / "tasks", secrets=_registry(secret))
    return store, lambda: store.create(subject="s", description=HUGE), \
        lambda: store.create(subject=f"deploy {secret}", description=f"k {secret}"), \
        tmp_path / "tasks"


def _cron(tmp_path, secret=SECRET):
    scheduler = CronScheduler(
        NoSessions(), durable_path=tmp_path / "cron.json", secrets=_registry(secret)
    )
    return scheduler, lambda: scheduler.schedule("s", "0 3 * * *", HUGE), \
        lambda: scheduler.schedule("s", "0 3 * * *", f"deploy {secret}"), \
        tmp_path


def _skills(tmp_path, secret=SECRET):
    root = tmp_path / "skills"
    (root / "s").mkdir(parents=True)

    def write(body):
        (root / "s" / "SKILL.md").write_text(
            f"---\nname: s\ndescription: d\n---\n{body}"
        )
        return SkillLoader(root)

    return None, lambda: write(HUGE), lambda: write(f"key {secret}"), root


STORES = {
    "memory": _memory,
    "teams": _teams,
    "tasks": _tasks,
    "cron": _cron,
    "skills": _skills,
}

#: Skills are files an operator places, not values an agent writes, so masking
#: them would edit somebody's source. They are bounded and reported instead.
NOT_MASKED = {"skills"}


@pytest.mark.parametrize("name", sorted(set(STORES) - {"skills"}))
def test_content_is_bounded(tmp_path, name):
    """Question 2. Every one of these fed a whole 2 MB somewhere it mattered."""
    _, oversized, _, root = STORES[name](tmp_path)
    oversized()

    biggest = max(
        (path.stat().st_size for path in root.rglob("*") if path.is_file()),
        default=0,
    )
    assert biggest < 200_000, (
        f"{name} stored {biggest:,} bytes from one write; the input was "
        f"{len(HUGE):,} characters"
    )


def test_a_skill_is_bounded_where_it_is_served(tmp_path):
    """Skills are files an operator writes, so the loader cannot bound the disk
    copy -- only what it hands to the model. Measuring the file would be
    measuring the operator's editor."""
    _, oversized, _, _ = STORES["skills"](tmp_path)
    loader = oversized()
    assert len(loader.load("s")) < 200_000
    assert any("truncated" in problem for problem in loader.problems)


@pytest.mark.parametrize("name", sorted(set(STORES) - NOT_MASKED))
@pytest.mark.parametrize("secret", [SECRET, ESCAPING_SECRET],
                         ids=["ascii", "json-escaping"])
def test_a_credential_does_not_reach_disk(tmp_path, name, secret):
    """Question 4. Sinks five through eleven were each found this way, one at
    a time; this asks all of them at once.

    The ``json-escaping`` variant is the one that matters here: a store that
    masks its *serialized* JSON rather than the structure searches the text for
    the secret's raw bytes, but `json.dumps` has already rewritten any non-ASCII
    or quote/backslash in it, so the mask slides straight past. Both `teams` and
    `tasks` masked after serializing and leaked this value while masking the
    ASCII one cleanly -- the sweep saw neither until it was asked with a
    credential that escapes.
    """
    _, _, with_secret, root = STORES[name](tmp_path, secret)
    with_secret()

    # The raw bytes and every form json.dumps could have rewritten them into:
    # if any survives, the value reached disk unmasked.
    forms = {secret, json.dumps(secret)[1:-1], json.dumps(secret, ensure_ascii=False)[1:-1]}
    leaked = [
        str(path.relative_to(root))
        for path in root.rglob("*")
        if path.is_file()
        for form in forms
        if form and form.encode() in path.read_bytes()
    ]
    assert not leaked, f"{name} wrote the {secret!r} credential to {leaked}"


@pytest.mark.parametrize("name", sorted(STORES))
def test_the_store_can_report_a_problem(tmp_path, name):
    """Question 3. A surface with nowhere to say 'that did not work' will
    eventually fail silently; every one of these has grown a `problems` list."""
    store, oversized, _, _ = STORES[name](tmp_path)
    if store is None:          # skills has no store object until it loads
        store = oversized()
    assert hasattr(store, "problems"), f"{name} has no way to report anything"


# --- and the specific failures this round found ---------------------------

def test_a_peer_cannot_deliver_an_unbounded_message(tmp_path):
    bus = MessageBus(tmp_path / "teams")
    assert bus.send("a", "t/b", HUGE).startswith("Error:")
    assert bus.read("t/b") == []


def test_a_mailbox_delivers_a_bounded_batch(tmp_path):
    bus = MessageBus(tmp_path / "teams")
    for index in range(2_000):
        bus.send("a", "t/c", f"msg {index}")
    delivered = bus.read("t/c")
    assert len(delivered) <= MessageBus.MAX_INBOX
    assert any("dropped" in problem for problem in bus.problems)


def test_reading_an_undrained_mailbox_does_not_load_the_whole_file(tmp_path):
    """The batch is bounded to MAX_INBOX, but the read that produced it was
    not: `read` loaded the entire mailbox with `read_text()`. A peer that keeps
    sending to a recipient that is busy, idle, or shut down grows the file
    without bound, and the eventual read OOMs the shared process. The read is
    now bounded to the tail -- the batch it delivers -- however large the file.
    """
    import tracemalloc

    bus = MessageBus(tmp_path / "teams")
    big = "X" * 15_000
    for index in range(2_500):  # ~38 MB accumulated, undrained
        bus.send("s", "t/victim", f"{index}:{big}")
    path = tmp_path / "teams" / "t" / "inboxes" / "victim.jsonl"
    file_bytes = path.stat().st_size

    tracemalloc.start()
    delivered = bus.read("t/victim")
    _current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()

    assert peak < file_bytes // 4, (
        f"read held {peak:,} bytes for a {file_bytes:,}-byte mailbox"
    )
    assert peak < MessageBus.MAX_READ_BYTES * 4, "the read scaled with the file"
    # Delivers the MOST RECENT batch (drop-oldest), and says it truncated.
    assert len(delivered) <= MessageBus.MAX_INBOX
    assert delivered[-1]["content"].startswith("2499:")
    assert any("dropped unread" in problem for problem in bus.problems)


def test_the_in_memory_mailbox_is_bounded_like_the_persisted_one(tmp_path):
    """The MAX_INBOX bound belongs to the mailbox, not the durable backend: the
    in-memory path returned every queued message while the persisted path
    capped -- the same 'a new path inherits the bound' gap the write-bound
    sweep kept finding.
    """
    bus = MessageBus()  # root=None -> in-memory backend
    # Inject past the queue directly so `read`'s own cap is what bounds the
    # batch, in isolation. Going through `send` would shed on the way in
    # (round 166), leaving the read cap untested -- a second, independent
    # backstop for any future append path that skips the send-side shed.
    bus.inboxes["b"] = [{"from": "s", "to": "b", "content": f"m{i}"} for i in range(500)]
    assert len(bus.read("b")) <= MessageBus.MAX_INBOX


def test_the_in_memory_mailbox_bounds_the_queue_not_only_the_read(tmp_path):
    """Capping what `read` *returns* is not the same as bounding what the mailbox
    *holds*. The queue grew one entry per message ever sent to a recipient that
    never drains -- a shut-down teammate, or a live one busy in a long turn --
    holding all of them in RAM while a read would only ever hand back the last
    MAX_INBOX. The queue itself is now shed on send, so peak memory tracks the
    bound; the reader still receives the same newest MAX_INBOX.
    """
    bus = MessageBus()  # in-memory backend
    for index in range(500):
        bus.send("s", "ghost", f"m{index}")

    # Checked *before* any read: the held queue, not the delivered batch.
    assert len(bus.inboxes["ghost"]) <= MessageBus.MAX_INBOX, (
        "the in-memory queue held every message ever sent, unbounded"
    )
    # The reader still gets the newest MAX_INBOX -- the shed ones are exactly the
    # ones the read cap would have dropped anyway.
    delivered = bus.read("ghost")
    assert delivered[-1]["content"] == "m499"
    assert len(delivered) == MessageBus.MAX_INBOX


def test_a_malformed_mailbox_read_is_reported(tmp_path):
    """`send` reported a bad key and `read` returned `[]`, so a typo looked
    exactly like an empty inbox -- to an agent waiting on it, forever."""
    bus = MessageBus(tmp_path / "teams")
    assert bus.read("../../escape") == []
    assert any("refused" in problem for problem in bus.problems)


def test_a_corrupt_message_line_is_reported(tmp_path):
    bus = MessageBus(tmp_path / "teams")
    bus.send("a", "t/d", "fine")
    path = tmp_path / "teams" / "t" / "inboxes" / "d.jsonl"
    path.write_text(path.read_text() + "{ not json\n")
    bus.read("t/d")
    assert any("malformed" in problem for problem in bus.problems)


def test_an_oversized_task_field_is_truncated(tmp_path):
    store = TaskStore(tmp_path / "tasks")
    task = store.create(subject="s", description=HUGE)
    assert len(store.load(task.id).description) < TaskStore.MAX_FIELD + 200


def test_an_ordinary_message_and_task_are_untouched(tmp_path):
    bus = MessageBus(tmp_path / "teams", secrets=_registry())
    bus.send("a", "t/e", "a normal message")
    assert bus.read("t/e")[0]["content"] == "a normal message"

    store = TaskStore(tmp_path / "tasks", secrets=_registry())
    task = store.create(subject="ship it", description="the details")
    assert store.load(task.id).description == "the details"
