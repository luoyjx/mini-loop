from __future__ import annotations

import copy
import hashlib
import json

import pytest

from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, text
from mini_loop.secrets import SecretRegistry
from mini_loop.skill_capture import (
    DEFAULT_DRAFT_TTL_SECONDS,
    DEFAULT_MAX_DRAFTS,
    DEFAULT_MAX_DRAFTS_PER_OWNER,
    DEFAULT_MAX_DRAFTS_PER_SESSION,
    PersonalSkillDraftStore,
    PersonalSkillError,
    preview_personal_skill,
    project_authenticated_turns,
    project_session_messages,
    record_personal_skill_turn,
)
from mini_loop.user_resources import canonical_user_skill
from mini_loop.token_efficiency import (
    ComponentDescriptor,
    ComponentStage,
    OptimizationMode,
    RequestOptimization,
    TokenEfficiencyRegistry,
)


SCHEMA = "mini-loop.personal-skill-draft/v1"


def _candidate(
    *,
    decision="create",
    description="Use when a repeatable review workflow is needed.",
    body="# Review workflow\n\n1. Inspect the inputs.\n2. Verify the result.",
    evidence=(0,),
    **extra,
):
    value = {
        "schema": SCHEMA,
        "decision": decision,
        "description": description,
        "body": body,
        "evidence_indexes": list(evidence),
    }
    value.update(extra)
    return value


def _response(value) -> FakeMessage:
    rendered = value if isinstance(value, str) else json.dumps(value)
    return FakeMessage([text(rendered)], "end_turn")


class _Agent:
    def __init__(self, responses, *, messages=None, secrets=None, store=None):
        self.responses = list(responses)
        self.messages = list(messages or [
            {"role": "user", "content": "Review this change carefully."},
            {"role": "assistant", "content": "I inspected it and verified the result."},
        ])
        self.secrets = secrets or SecretRegistry()
        self.state = {"session_id": "session-a"}
        if store is not None:
            self.state["personal_skill_drafts"] = store
        self.calls = []

    async def _create(self, messages, **kwargs):
        self.calls.append((copy.deepcopy(messages), dict(kwargs)))
        response = self.responses.pop(0)
        if isinstance(response, BaseException):
            raise response
        return response


class _PreviewInjector:
    descriptor = ComponentDescriptor(
        id="preview-injector",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    def __init__(self, value):
        self.value = value
        self.calls = 0

    async def optimize(self, context, *, budget_tokens=None):
        self.calls += 1
        return RequestOptimization(
            {"messages": [{"role": "user", "content": self.value}]}
        )


class _PreviewCacheInjector:
    def __init__(self, value):
        self.value = value
        self.calls = 0

    def annotate(self, *, system, tools, messages):
        self.calls += 1
        return system, tools, [{"role": "user", "content": self.value}]


def _add(store, name, *, owner="alice", session_id="session-a"):
    return store.add(
        owner=owner,
        session_id=session_id,
        name=name,
        description=f"Use {name} for a repeatable workflow.",
        body=f"# {name}\n\n1. Do the work.\n2. Verify it.",
        evidence_indexes=(0,),
        coverage="current_epoch",
        omitted=0,
    )


def test_projection_keeps_only_clean_text_and_does_not_mutate_transcript():
    messages = [
        {
            "role": "user",
            "content": (
                "<memory_context>\nPRIVATE MEMORY\n</memory_context>\n\n"
                "actual request"
            ),
        },
        {
            "role": "assistant",
            "content": [
                {"type": "text", "text": "ordinary answer"},
                {"type": "tool_use", "id": "t1", "name": "load_skill"},
                {"type": "text", "text": "second answer"},
            ],
        },
        {
            "role": "user",
            "content": [
                {"type": "text", "text": "USER LIST TEXT"},
                {
                    "type": "tool_result",
                    "tool_use_id": "t1",
                    "content": "RAW TOOL AND SKILL BODY",
                },
            ],
        },
        {"role": "user", "content": "<runtime-state>INDEX</runtime-state>"},
        {
            "role": "user",
            "content": '<task_notification id="x">BACKGROUND</task_notification>',
        },
        {"role": "user", "content": "<team_inbox>TEAM</team_inbox>"},
        {
            "role": "user",
            "content": "<workflow-results>WORKFLOW</workflow-results>",
        },
        {"role": "user", "content": "[Scheduled cron abc] unattended"},
        {
            "role": "user",
            "content": "[Context compressed. Full transcript: private] summary",
        },
        {"role": "user", "content": "[SNIPPED oversized result]"},
        {
            "role": "user",
            "content": "<user_interjection>\nsteered request\n</user_interjection>",
        },
        {"role": "system", "content": "SYSTEM BODY"},
    ]
    original = copy.deepcopy(messages)

    projection = project_session_messages(messages)

    assert projection["messages"] == [
        {"role": "user", "content": "actual request"},
        {"role": "assistant", "content": "ordinary answer\nsecond answer"},
        {"role": "user", "content": "steered request"},
    ]
    assert projection["coverage"] == "current_epoch"
    assert projection["omitted"] == 0
    assert projection["compacted_history_excluded"] is True
    assert messages == original
    assert "PRIVATE MEMORY" not in json.dumps(projection)
    assert "RAW TOOL" not in json.dumps(projection)


def test_projection_masks_json_escaping_secret_before_serialization():
    secret = 'clé-secrète-"café"\\Ω-123456'
    registry = SecretRegistry()
    registry.register("CAPTURE_TOKEN", secret)
    messages = [
        {"role": "user", "content": f"Use {secret} in this workflow"},
        {"role": "assistant", "content": f"I saw {secret}"},
    ]

    projection = project_session_messages(messages, registry)
    rendered = json.dumps(projection, ensure_ascii=True)

    assert secret not in rendered
    assert json.dumps(secret)[1:-1] not in rendered
    assert rendered.count("<secret-hidden>") == 2


def test_projection_uses_the_final_memory_close_so_body_cannot_escape():
    messages = [
        {
            "role": "user",
            "content": (
                "<memory_context>\nremembered preface\n</memory_context>\n"
                "PRIVATE_MEMORY_CANARY\n</memory_context>\n\nactual request"
            ),
        }
    ]

    projection = project_session_messages(messages)

    assert projection["messages"] == [
        {"role": "user", "content": "actual request"}
    ]
    assert "PRIVATE_MEMORY_CANARY" not in json.dumps(projection)


def test_authenticated_projection_uses_provenance_not_wrapper_blacklists():
    turns = [
        {
            "role": "user",
            "content": "<memory_context>literal human text</memory_context>",
        },
        {"role": "assistant", "content": "[Error] literal final answer"},
    ]

    projection = project_authenticated_turns(turns)

    assert projection["messages"] == turns
    assert projection["coverage"] == "authenticated_turns"


def test_authenticated_turn_ledger_is_masked_and_provenance_only():
    secret = "long-personal-capture-token"
    registry = SecretRegistry()
    registry.register("CAPTURE_TOKEN", secret)
    agent = _Agent([], secrets=registry)
    agent.state["personal_skill_turns"] = []
    agent.state["personal_skill_turns_omitted"] = 0
    agent.messages.extend(
        [
            {"role": "user", "content": "[Goal round 3] injected continuation"},
            {"role": "user", "content": "CUSTOM_INJECTOR_CANARY"},
        ]
    )

    record_personal_skill_turn(
        agent,
        f"human request with {secret}",
        "verified final answer",
    )

    rendered = json.dumps(agent.state["personal_skill_turns"])
    assert secret not in rendered
    assert "<secret-hidden>" in rendered
    assert "CUSTOM_INJECTOR_CANARY" not in rendered


def test_authenticated_turn_ledger_is_bounded_to_whole_messages():
    agent = _Agent([])
    agent.state["personal_skill_turns"] = []
    agent.state["personal_skill_turns_omitted"] = 0

    for index in range(40):
        record_personal_skill_turn(agent, f"request-{index}", f"answer-{index}")

    ledger = agent.state["personal_skill_turns"]
    assert len(ledger) == 64
    assert ledger[0] == {"role": "user", "content": "request-8"}
    assert ledger[-1] == {"role": "assistant", "content": "answer-39"}
    assert agent.state["personal_skill_turns_omitted"] == 16


@pytest.mark.parametrize("secret", ["1234", lambda: None])
def test_authenticated_turn_ledger_fails_closed_for_unscreenable_secrets(secret):
    registry = SecretRegistry()
    registry.register("CAPTURE_TOKEN", secret)
    agent = _Agent([], secrets=registry)
    agent.state["personal_skill_turns"] = []

    record_personal_skill_turn(agent, "human request", "verified answer")

    assert agent.state["personal_skill_turns"] == []
    assert agent.state["personal_skill_capture_error"] == (
        "secret_screening_unavailable"
    )


def test_projection_keeps_a_suffix_of_whole_messages_with_a_receipt():
    messages = [
        {"role": "user", "content": f"message-{index}-" + "x" * 30}
        for index in range(5)
    ]
    encoded = [
        json.dumps(message, ensure_ascii=False, separators=(",", ":"), sort_keys=True)
        for message in messages
    ]
    limit = 2 + len(encoded[-1]) + 1 + len(encoded[-2])

    projection = project_session_messages(messages, max_chars=limit)

    assert projection["messages"] == messages[-2:]
    assert projection["coverage"] == "current_epoch_tail"
    assert projection["omitted"] == 3
    assert all(message["content"].endswith("x" * 30) for message in projection["messages"])


@pytest.mark.asyncio
async def test_preview_uses_strict_side_query_and_stores_canonical_digest():
    store = PersonalSkillDraftStore()
    agent = _Agent([_response(_candidate())], store=store)

    draft = await preview_personal_skill(
        agent, "alice", "review-changes", focus="preserve verification"
    )

    assert agent.state["personal_skill_drafts"] is store
    [(_, kwargs)] = agent.calls
    assert kwargs["tools"] == []
    assert kwargs["purpose"] == "personal_skill_preview"
    assert kwargs["immutable_messages"] is True
    assert kwargs["max_tokens"] == 2_500
    assert "requested_name" in agent.calls[0][0][0]["content"]
    canonical = canonical_user_skill(draft.name, draft.description, draft.body)
    assert draft.digest == hashlib.sha256(canonical.encode()).hexdigest()
    assert store.get(
        draft.draft_id,
        owner="alice",
        session_id="session-a",
        digest=draft.digest,
    ) is draft
    public = draft.public_dict()
    assert not ({"owner", "session_id", "path", "root"} & public.keys())
    assert public["coverage"] == "current_epoch"


@pytest.mark.asyncio
async def test_preview_prefers_authenticated_turn_ledger_over_transcript_injectors():
    agent = _Agent(
        [_response(_candidate(evidence=(0, 1)))],
        messages=[
            {"role": "user", "content": "CUSTOM_INJECTOR_CANARY"},
            {"role": "user", "content": "[Goal round 3] continuation"},
            {"role": "assistant", "content": "[Error] internal failure"},
        ],
    )
    agent.state["personal_skill_turns"] = [
        {"role": "user", "content": "human reviewed the workflow"},
        {"role": "assistant", "content": "final verified result"},
    ]
    agent.state["personal_skill_turns_omitted"] = 2
    agent.state["personal_skill_compacted_history_excluded"] = True

    draft = await preview_personal_skill(agent, "alice", "review-changes")

    payload = json.loads(agent.calls[0][0][0]["content"])
    rendered = json.dumps(payload)
    assert payload["messages"] == agent.state["personal_skill_turns"]
    assert payload["coverage"] == "authenticated_turns_tail"
    assert payload["omitted"] == 2
    assert payload["compacted_history_excluded"] is True
    assert draft.coverage == "authenticated_turns_tail"
    assert "CUSTOM_INJECTOR_CANARY" not in rendered
    assert "Goal round" not in rendered
    assert "internal failure" not in rendered


@pytest.mark.asyncio
async def test_preview_provider_payload_skips_mutable_request_projection_seams(
    tmp_path,
):
    secret = "registered-preview-secret-123456"
    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text(json.dumps(_candidate()))], "end_turn"

    secrets = SecretRegistry()
    secrets.register("PREVIEW_TOKEN", secret)
    optimizer = _PreviewInjector(secret)
    components = TokenEfficiencyRegistry()
    components.register_request_optimizer(
        optimizer,
        mode=OptimizationMode.ENFORCE,
    )
    cache_policy = _PreviewCacheInjector(secret)
    agent = Agent(
        client=FakeAsyncAnthropic(responder=responder, thinking=False),
        settings=Settings(),
        workspace=tmp_path,
        secrets=secrets,
        token_efficiency=components.runtime(),
        cache_policy=cache_policy,
        state={
            "session_id": "session-a",
            "personal_skill_turns": [
                {"role": "user", "content": "safe reviewed request"}
            ],
            "personal_skill_turns_omitted": 0,
            "personal_skill_drafts": PersonalSkillDraftStore(),
        },
    )

    await preview_personal_skill(agent, "alice", "review-workflow")

    assert optimizer.calls == 0
    assert cache_policy.calls == 0
    assert secret not in json.dumps(seen)


@pytest.mark.asyncio
async def test_corrupt_managed_ledger_never_falls_back_to_raw_transcript():
    agent = _Agent(
        [_response(_candidate())],
        messages=[{"role": "user", "content": "RAW_TRANSCRIPT_INJECTOR_CANARY"}],
    )
    agent.state["personal_skill_turns"] = {"corrupt": True}

    with pytest.raises(PersonalSkillError) as caught:
        await preview_personal_skill(agent, "alice", "review-workflow")

    assert caught.value.code == "capture_source_unavailable"
    assert caught.value.status_code == 503
    assert agent.calls == []


@pytest.mark.asyncio
@pytest.mark.parametrize("secret", ["1234", lambda: None])
async def test_preview_fails_before_side_query_when_secret_screening_is_unavailable(
    secret,
):
    registry = SecretRegistry()
    registry.register("PREVIEW_TOKEN", secret)
    agent = _Agent([_response(_candidate())], secrets=registry)

    with pytest.raises(PersonalSkillError) as caught:
        await preview_personal_skill(agent, "alice", "review-changes")

    assert caught.value.code == "secret_screening_unavailable"
    assert caught.value.status_code == 503
    assert agent.calls == []


@pytest.mark.asyncio
async def test_malformed_response_gets_one_fresh_repair_without_echoing_it():
    marker = "RAW-INVALID-DRAFT-MARKER"
    agent = _Agent([
        _response(f"```json\n{marker}\n```"),
        _response(_candidate()),
    ])

    draft = await preview_personal_skill(agent, "alice", "review-changes")

    assert draft.name == "review-changes"
    assert len(agent.calls) == 2
    repair_prompt = agent.calls[1][0][0]["content"]
    assert "repair" in repair_prompt
    assert marker not in repair_prompt


@pytest.mark.asyncio
async def test_strict_schema_rejects_extra_fields_then_repairs_once():
    agent = _Agent([
        _response(_candidate(unexpected="not allowed")),
        _response(_candidate()),
    ])

    await preview_personal_skill(agent, "alice", "review-changes")

    assert len(agent.calls) == 2


@pytest.mark.asyncio
async def test_skip_is_a_named_noop_and_does_not_create_a_draft():
    store = PersonalSkillDraftStore()
    agent = _Agent([
        _response(_candidate(decision="skip", description="", body="", evidence=()))
    ], store=store)

    with pytest.raises(PersonalSkillError) as caught:
        await preview_personal_skill(agent, "alice", "review-changes")

    assert caught.value.code == "preview_skipped"
    assert caught.value.status_code == 422
    assert not store._drafts
    assert len(agent.calls) == 1


@pytest.mark.asyncio
async def test_secret_in_projection_focus_and_first_output_is_repaired_safely():
    secret = 'clé-preview-"café"\\Ω-123456'
    registry = SecretRegistry()
    registry.register("PREVIEW_TOKEN", secret)
    first = _candidate(body=f"# Unsafe\n\nUse {secret}")
    agent = _Agent(
        [_response(first), _response(_candidate())],
        messages=[{"role": "user", "content": f"Workflow used {secret}"}],
        secrets=registry,
    )

    draft = await preview_personal_skill(
        agent, "alice", "review-changes", focus=f"focus {secret}"
    )

    assert secret not in draft.body
    assert len(agent.calls) == 2
    for request, _kwargs in agent.calls:
        rendered = json.dumps(request, ensure_ascii=True)
        assert secret not in rendered
        assert json.dumps(secret)[1:-1] not in rendered


@pytest.mark.asyncio
async def test_second_sensitive_or_malformed_output_fails_with_zero_drafts():
    secret = "sk-preview-secret-123456789"
    registry = SecretRegistry()
    registry.register("PREVIEW_TOKEN", secret)
    unsafe = _response(_candidate(body=f"# Unsafe\n\n{secret}"))
    store = PersonalSkillDraftStore()
    agent = _Agent([unsafe, unsafe], secrets=registry, store=store)

    with pytest.raises(PersonalSkillError) as caught:
        await preview_personal_skill(agent, "alice", "review-changes")

    assert caught.value.code == "invalid_preview"
    assert len(agent.calls) == 2
    assert not store._drafts


@pytest.mark.asyncio
async def test_provider_failure_is_not_a_second_semantic_retry():
    store = PersonalSkillDraftStore()
    agent = _Agent([RuntimeError("provider details")], store=store)

    with pytest.raises(PersonalSkillError) as caught:
        await preview_personal_skill(agent, "alice", "review-changes")

    assert caught.value.code == "provider_failure"
    assert caught.value.status_code == 502
    assert len(agent.calls) == 1
    assert not store._drafts
    assert "provider details" not in str(caught.value)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "name",
    ["Uppercase", "two_words", "two--words", "-leading", "x" * 65],
)
async def test_invalid_name_is_rejected_before_the_llm(name):
    agent = _Agent([_response(_candidate())])

    with pytest.raises(PersonalSkillError) as caught:
        await preview_personal_skill(agent, "alice", name)

    assert caught.value.code == "invalid_name"
    assert agent.calls == []


def test_store_binds_owner_session_and_digest_and_consume_is_one_shot():
    now = [100.0]
    store = PersonalSkillDraftStore(clock=lambda: now[0])
    draft = _add(store, "review-changes")

    for owner, session_id in (("bob", "session-a"), ("alice", "session-b")):
        with pytest.raises(PersonalSkillError) as caught:
            store.get(
                draft.draft_id,
                owner=owner,
                session_id=session_id,
            )
        assert caught.value.code == "draft_not_found"
        assert caught.value.status_code == 404

    with pytest.raises(PersonalSkillError) as mismatch:
        store.consume(
            draft.draft_id,
            owner="alice",
            session_id="session-a",
            digest="0" * 64,
        )
    assert mismatch.value.code == "draft_digest_mismatch"
    assert store.peek(
        draft.draft_id, owner="alice", session_id="session-a"
    ) is draft

    consumed = store.consume(
        draft.draft_id,
        owner="alice",
        session_id="session-a",
        digest=draft.digest,
    )
    assert consumed is draft
    with pytest.raises(PersonalSkillError, match="draft not found"):
        store.get(draft.draft_id, owner="alice", session_id="session-a")


def test_expired_draft_is_hidden_from_other_authorities():
    now = [0.0]
    store = PersonalSkillDraftStore(ttl_seconds=1, clock=lambda: now[0])
    draft = _add(store, "expiring-skill")
    now[0] = 2.0

    with pytest.raises(PersonalSkillError) as stranger:
        store.get(draft.draft_id, owner="bob", session_id="session-a")
    assert stranger.value.code == "draft_not_found"
    assert stranger.value.status_code == 404

    with pytest.raises(PersonalSkillError) as owner:
        store.get(draft.draft_id, owner="alice", session_id="session-a")
    assert owner.value.code == "draft_expired"
    assert owner.value.status_code == 410


def test_committed_draft_cleanup_cannot_make_success_ambiguous_after_expiry():
    now = [0.0]
    store = PersonalSkillDraftStore(ttl_seconds=1, clock=lambda: now[0])
    draft = _add(store, "durable-result")
    assert store.peek(
        draft.draft_id,
        owner="alice",
        session_id="session-a",
        digest=draft.digest,
    ) is draft

    now[0] = 2.0
    assert store.discard_committed(draft) is True
    assert store.discard_committed(draft) is False


def test_store_expiry_and_global_and_per_session_bounds():
    now = [100.0]
    store = PersonalSkillDraftStore(
        ttl_seconds=10,
        max_items=3,
        max_per_session=2,
        clock=lambda: now[0],
    )
    first = _add(store, "first-skill")
    second = _add(store, "second-skill")
    third = _add(store, "third-skill")
    with pytest.raises(PersonalSkillError):
        store.get(first.draft_id, owner="alice", session_id="session-a")
    assert store.get(second.draft_id, owner="alice", session_id="session-a")
    assert store.get(third.draft_id, owner="alice", session_id="session-a")

    bob_one = _add(store, "bob-one", owner="bob", session_id="session-b")
    bob_two = _add(store, "bob-two", owner="bob", session_id="session-b")
    assert store.get(second.draft_id, owner="alice", session_id="session-a")
    assert store.get(third.draft_id, owner="alice", session_id="session-a")
    with pytest.raises(PersonalSkillError):
        store.get(bob_one.draft_id, owner="bob", session_id="session-b")
    assert store.get(bob_two.draft_id, owner="bob", session_id="session-b")

    now[0] = 111.0
    with pytest.raises(PersonalSkillError) as expired:
        store.get(bob_two.draft_id, owner="bob", session_id="session-b")
    assert expired.value.code == "draft_expired"
    assert expired.value.status_code == 410


def test_global_capacity_never_evicts_another_owner_draft():
    store = PersonalSkillDraftStore(
        max_items=2,
        max_per_owner=1,
        max_per_session=1,
    )
    alice = _add(
        store, "alice-skill", owner="alice", session_id="alice-session"
    )
    bob = _add(store, "bob-skill", owner="bob", session_id="bob-session")

    with pytest.raises(PersonalSkillError) as full:
        _add(
            store,
            "charlie-skill",
            owner="charlie",
            session_id="charlie-session",
        )

    assert full.value.code == "draft_capacity"
    assert full.value.status_code == 429
    assert store.get(
        alice.draft_id, owner="alice", session_id="alice-session"
    ) is alice
    assert store.get(bob.draft_id, owner="bob", session_id="bob-session") is bob


def test_store_defaults_are_fifteen_minutes_global_64_and_per_session_4():
    store = PersonalSkillDraftStore()
    assert store.ttl_seconds == DEFAULT_DRAFT_TTL_SECONDS == 900
    assert store.max_items == DEFAULT_MAX_DRAFTS == 64
    assert store.max_per_owner == DEFAULT_MAX_DRAFTS_PER_OWNER == 16
    assert store.max_per_session == DEFAULT_MAX_DRAFTS_PER_SESSION == 4
