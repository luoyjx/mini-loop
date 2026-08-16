"""Break each hardening on purpose and require a named test to notice.

A guard is a claim: "if this protection is removed, the suite fails". The claim
was being checked by hand, with `git stash` and a re-run -- and in round 34 that
stash silently did nothing, reported ten passes, and would have been read as
"the guards do not work" if the result had not been implausible enough to
question. A verification that can no-op is not a verification.

This does it deterministically: copy the tree, apply one precise mutation to the
copy, run the test that is supposed to catch it, and require a failure. The
working tree is never touched, so an interrupted run cannot leave the repo in a
half-mutated state.

    python tools/verify_guards.py [-k substring]

A mutation that does *not* fail its test is reported as SURVIVED: either the
guard is vacuous or it is pinned somewhere other than where it is documented.
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


@dataclass(frozen=True)
class Mutation:
    name: str
    round: int
    file: str
    old: str
    new: str
    test: str
    claim: str


MUTATIONS = [
    Mutation(
        "block-normalization-is-lossy", 30, "mini_loop/agent.py",
        'fields["type"] = getattr(block, "type", "unknown")\n            payload.append(fields)',
        'payload.append({"type": getattr(block, "type", "unknown")})',
        "tests/test_provider_fidelity.py",
        "an unrecognized block keeps its fields, so a thinking signature survives",
    ),
    Mutation(
        "microcompact-mutates-in-place", 27, "mini_loop/compaction.py",
        'content[part_index] = {**part, "content": "[cleared]"}',
        'part["content"] = "[cleared]"',
        "tests/test_compaction_composition.py",
        "compaction replaces messages so the store mirrors it",
    ),
    Mutation(
        "budget-spill-mutates-in-place", 122, "mini_loop/compaction.py",
        "    new_content = [\n        {**part, \"content\": replacements[position]}\n        if position in replacements else part\n        for position, part in enumerate(content)\n    ]\n    messages[target_index] = {**messages[target_index], \"content\": new_content}\n    return len(replacements)",
        "    for position, replacement in replacements.items():\n        content[position][\"content\"] = replacement\n    return len(replacements)",
        "tests/test_compaction_composition.py::test_every_rewriter_is_mirrored_to_the_store",
        "tool_result_budget replaces the message object so the rewrite detector "
        "sees it and the store mirrors the spill; editing the block in place "
        "leaves a restart with the un-budgeted transcript",
    ),
    Mutation(
        "mirroring-roster-omits-a-rewriter", 122, "tests/test_compaction_composition.py",
        '    "tool_result_budget": lambda agent: tool_result_budget(\n        agent.messages, agent.workspace, max_bytes=200, preview_chars=50\n    ),',
        "",
        "tests/test_compaction_composition.py::test_the_mirroring_roster_covers_every_rewriter_maybe_compact_runs",
        "the completeness guard fails when the roster omits a rewriter "
        "maybe_compact runs -- the omission that hid the budget-spill bug",
    ),
    Mutation(
        "strategy-roster-omits-a-compactor", 123, "tests/test_compaction_composition.py",
        "STRATEGIES = [DefaultCompactor, InMemoryCompactor]",
        "STRATEGIES = [DefaultCompactor]",
        "tests/test_compaction_composition.py::test_the_strategy_roster_covers_every_compactor_the_module_ships",
        "the completeness guard fails when the strategy roster omits a shipped "
        "compactor, so no new strategy escapes the secret-leak sweep unnoticed",
    ),
    Mutation(
        "summary-read-by-attribute", 32, "mini_loop/compaction.py",
        "summary = block_text(resp.content)",
        'summary = "".join(getattr(b, "text", "") for b in resp.content '
        'if getattr(b, "type", "") == "text")',
        "tests/test_block_access.py",
        "the summary that replaces a transcript is read shape-agnostically",
    ),
    Mutation(
        "spill-is-unmasked", 27, "mini_loop/compaction.py",
        "            path, secrets.mask(original) if secrets is not None else original",
        "            path, original",
        "tests/test_compaction_composition.py",
        "context spilled to the workspace carries no credential",
    ),
    Mutation(
        "injector-return-unchecked", 33, "mini_loop/agent.py",
        "self.messages.extend(_injected_messages(extra, inject))",
        "self.messages.extend(extra)",
        "tests/test_extension_contracts.py",
        "an injector returning a string cannot shred the transcript",
    ),
    Mutation(
        "cache-tokens-uncounted", 29, "mini_loop/metering.py",
        'PROMPT_TOKEN_FIELDS = (\n    "input_tokens",\n    "cache_read_input_tokens",\n    "cache_creation_input_tokens",\n)',
        'PROMPT_TOKEN_FIELDS = ("input_tokens",)',
        "tests/test_metering.py",
        "cached tokens count toward the prompt",
    ),
    Mutation(
        "compaction-uses-the-estimate", 29, "mini_loop/compaction.py",
        "    meter = getattr(agent, \"token_meter\", None)\n    if meter is None:\n        return estimate_tokens(agent.messages)",
        "    return estimate_tokens(agent.messages)\n    meter = getattr(agent, \"token_meter\", None)\n    if meter is None:\n        return estimate_tokens(agent.messages)",
        "tests/test_metering.py",
        "compaction fires on the provider's count, not a guess",
    ),
    Mutation(
        "meter-blind-to-a-shrink", 119, "mini_loop/metering.py",
        "        delta = estimate - self._anchor_estimate\n        return max(0, int(self._anchor_actual + delta * self._calibration))",
        "        growth = max(0, estimate - self._anchor_estimate)\n        return int(self._anchor_actual + growth * self._calibration)",
        "tests/test_metering.py::test_cheap_compaction_below_threshold_skips_the_llm_summary",
        "clamping the meter's delta to growth hides in-process compaction, so the "
        "expensive LLM-summary layer runs on a transcript snip already shrank",
    ),
    Mutation(
        "continuation-returns-the-tail", 31, "mini_loop/recovery.py",
        "            if chunks:\n                # Hand back the whole answer, not the tail of it.\n                return ContinuedResponse([*chunks, _content_payload(resp.content)], resp)",
        "            if chunks:\n                pass",
        "tests/test_continuation.py",
        "a continued answer reaches the caller whole",
    ),
    Mutation(
        "failing-store-is-silent", 34, "mini_loop/identity.py",
        '        "state_store_error": (\n            manager.persistence_error()\n            if hasattr(manager, "persistence_error") else None\n        ),',
        "",
        "tests/test_degraded_but_silent.py",
        "a store that is installed but failing is reported",
    ),
    Mutation(
        "token-comparison-short-circuits", 0, "mini_loop/auth.py",
        "            if hmac.compare_digest(token, presented):\n                matched = principal_id",
        "            if token == presented:\n                matched = principal_id",
        "tests/test_timing_safety.py",
        "bearer tokens are compared in constant time",
    ),
    Mutation(
        "token-match-short-circuits", 35, "mini_loop/auth.py",
        "            if hmac.compare_digest(token, presented):\n                matched = principal_id",
        "            if hmac.compare_digest(token, presented):\n                return Principal(id=principal_id)",
        "tests/test_timing_safety.py",
        "every token is compared, so position is not observable",
    ),
    Mutation(
        "shared-token-silently-collapses", 142, "mini_loop/auth.py",
        "            if token in tokens and tokens[token] != principal_id:",
        "            if False:",
        "tests/test_auth.py::test_a_token_shared_by_two_principals_is_refused",
        "a token assigned to two principals is refused, not silently collapsed "
        "to last-wins: a shared credential lets one caller authenticate as "
        "another",
    ),
    # --- security-critical hardenings, previously unverified -------------
    Mutation(
        "sandbox-interpolates-paths", 22, "mini_loop/sandbox.py",
        'argv = [SANDBOX_EXEC, "-p", policy]\n        for param in params:\n            argv += ["-D", param]',
        'for param in params:\n            key, _, value = param.partition("=")\n            policy = policy.replace(f\'(param "{key}")\', f\'"{value}"\')\n        argv = [SANDBOX_EXEC, "-p", policy]',
        "tests/test_unpinned_claims.py",
        "workspace paths are policy parameters, never interpolated into it",
    ),
    Mutation(
        "sandbox-binary-from-path", 22, "mini_loop/sandbox.py",
        'SANDBOX_EXEC = "/usr/bin/sandbox-exec"',
        'SANDBOX_EXEC = "sandbox-exec"',
        "tests/test_sandbox.py",
        "the confinement binary is absolute, not resolved through PATH",
    ),
    Mutation(
        "lease-take-is-unconditional", 23, "mini_loop/storage.py",
        "                   AND (lease_owner IS NULL OR lease_owner = ? OR lease_until < ?)\n                \"\"\",\n                (owner, now + ttl, session_id, owner, now),",
        "                \"\"\",\n                (owner, now + ttl, session_id),",
        "tests/test_leases.py",
        "a lease is only taken when free, expired, or already ours",
    ),
    Mutation(
        "schema-downgrade-is-silent", 23, "mini_loop/storage.py",
        "                raise StorageSchemaError(",
        "                pass\n            if False:\n                raise StorageSchemaError(",
        "tests/test_storage.py",
        "a database written by a newer schema is refused, not silently truncated",
    ),
    Mutation(
        "migration-forgets-a-column", 135, "mini_loop/storage.py",
        '            if "todos" not in columns:',
        "            if False:",
        "tests/test_storage.py::test_an_upgraded_database_ends_identical_to_a_fresh_one",
        "an old database upgrades to a schema identical to a fresh one; a column "
        "added to _SCHEMA but forgotten in _upgrade is silently absent from every "
        "upgraded database (CREATE IF NOT EXISTS is a no-op on an existing table)",
    ),
    Mutation(
        "ownership-leaks-existence", 24, "mini_loop/server.py",
        'if session is None or getattr(session, "owner", ANONYMOUS.id) != caller.id:',
        "if session is None:",
        "tests/test_auth.py",
        "another caller's session is 404, and is not reachable at all",
    ),
    Mutation(
        "sse-token-works-everywhere", 24, "mini_loop/server.py",
        'if request.url.path.endswith("/events"):\n        token = request.query_params.get("access_token")',
        'if True:\n        token = request.query_params.get("access_token")',
        "tests/test_auth.py",
        "a URL credential is accepted only on the SSE route",
    ),
    Mutation(
        "reconcile-accepts-any-state", 24, "mini_loop/actions.py",
        "        if status not in TERMINAL_STATUSES:\n            raise ValueError(f\"cannot reconcile to {status!r}\")",
        "        if False:\n            raise ValueError(f\"cannot reconcile to {status!r}\")",
        "tests/test_unpinned_claims.py",
        "reconciliation only writes a status the rest of the system handles",
    ),
    Mutation(
        "tool-input-unmasked", 21, "mini_loop/agent.py",
        "input=self.secrets.mask_payload(call.input),",
        "input=call.input,",
        "tests/test_write_sites.py",
        "a credential in a tool argument is masked at the agent boundary too",
    ),
    Mutation(
        "path-check-skips-symlinks", 37, "mini_loop/tools.py",
        "path = (self.workspace / p).resolve()",
        "path = Path(os.path.normpath(self.workspace / p))",
        "tests/test_shell_blocklist_limits.py",
        "a symlink planted in the workspace does not lead out of it",
    ),
    Mutation(
        "blocklist-reads-as-confinement", 37, "mini_loop/audit.py",
        '"DANGEROUS command list is a typo guard, not confinement -- it "',
        '"DANGEROUS command list also applies -- "',
        "tests/test_shell_blocklist_limits.py",
        "the audit refuses to let the blocklist read as a control",
    ),
    Mutation(
        "escalation-ignores-the-ceiling", 38, "mini_loop/recovery.py",
        'ceiling = None if streaming else nonstreaming_ceiling(kwargs.get("model", ""))\n                target = min(ESCALATED_MAX_TOKENS, ceiling) if ceiling else ESCALATED_MAX_TOKENS',
        "target = ESCALATED_MAX_TOKENS",
        "tests/test_token_escalation.py",
        "escalation never asks for more than a non-streaming call can carry",
    ),
    Mutation(
        "refused-escalation-loses-the-partial", 38, "mini_loop/recovery.py",
        "                    if escalation_partial is not None:",
        "                    if False:",
        "tests/test_token_escalation.py",
        "a refused escalation keeps the work already paid for",
    ),
    Mutation(
        "deltas-are-persisted", 39, "mini_loop/session.py",
        'ephemeral = bool(event.pop("_ephemeral", False))',
        'event.pop("_ephemeral", False)\n        ephemeral = False',
        "tests/test_streaming.py",
        "streamed progress never reaches the durable log",
    ),
    Mutation(
        "streaming-still-capped", 39, "mini_loop/recovery.py",
        'ceiling = None if streaming else nonstreaming_ceiling(kwargs.get("model", ""))',
        'ceiling = nonstreaming_ceiling(kwargs.get("model", ""))',
        "tests/test_streaming.py",
        "streaming removes the non-streaming ceiling",
    ),
    Mutation(
        "stream-deltas-unmasked", 39, "mini_loop/transport.py",
        "text=agent.secrets.mask(text),",
        "text=text,",
        "tests/test_streaming.py",
        "a credential in a streamed delta is masked",
    ),
    Mutation(
        "completed-stream-leaves-stale-partial", 130, "mini_loop/transport.py",
        '            agent.streamed_text = ""\n            return final',
        "            return final",
        "tests/test_interrupted_turns.py::test_a_completed_stream_leaves_no_partial_to_re_record",
        "a completed stream clears streamed_text, so an interrupt after it records "
        "only the marker; left set, it re-records the round (or the compaction "
        "summary) as a phantom interrupted turn",
    ),
    Mutation(
        "dropped-stream-not-retried", 40, "mini_loop/recovery.py",
        "    return is_overloaded(e) or is_rate_limit(e) or is_connection_error(e)",
        "    return is_overloaded(e) or is_rate_limit(e)",
        "tests/test_streaming_failures.py",
        "a stream that drops mid-flight is retried, not lost",
    ),
    Mutation(
        "deltas-are-replayed", 40, "mini_loop/session.py",
        'if not event.get("ephemeral"):\n            self._backlog.append(event)',
        "self._backlog.append(event)",
        "tests/test_streaming_failures.py",
        "stale progress is not replayed to a late subscriber",
    ),
    Mutation(
        "retry-does-not-announce-itself", 40, "mini_loop/transport.py",
        'await agent._send(\n'
        '            "stream_start",\n'
        '            stream_id=stream_id,\n'
        '            phase="commentary",\n'
        '            provisional=True,\n'
        '            _ephemeral=True,\n'
        '        )',
        "pass",
        "tests/test_streaming_failures.py",
        "a regenerating retry tells the console to start over",
    ),
    Mutation(
        "interruption-leaves-no-trace", 41, "mini_loop/session.py",
        "        shown = self._record_interruption(reason, repaired)",
        "        shown = False",
        "tests/test_interrupted_turns.py",
        "an interrupted turn records that it was interrupted",
    ),
    Mutation(
        "thinking-replayed-as-answer", 41, "mini_loop/transport.py",
        'piece for piece, kind in pending if kind == "text"',
        "piece for piece, kind in pending",
        "tests/test_interrupted_turns.py",
        "thinking is shown as progress and never recorded as answer text",
    ),
    # The three historical thinnesses of the offline model, re-created. Each was
    # found by a different accident; the guard has to find them by construction.
    Mutation(
        "double-drops-usage", 29, "mini_loop/fake_llm.py",
        "        self.usage = usage or FakeUsage(0)",
        "        pass",
        "tests/test_provider_surface.py",
        "the double answers every read the package makes off a response",
    ),
    Mutation(
        "double-conflates-delta-types", 41, "mini_loop/fake_llm.py",
        '        self.type = f"{field}_delta"\n        setattr(self, field, body)',
        '        self.type = "text_delta"\n        self.text = body\n        self.thinking = body',
        "tests/test_provider_surface.py",
        "a delta carries exactly one body field, so consumers can tell them apart",
    ),
    Mutation(
        "double-drops-the-stream", 39, "mini_loop/fake_llm.py",
        "    def stream(self, **kwargs):\n        return _FakeStream(self._parent, kwargs)",
        "    pass",
        "tests/test_provider_surface.py",
        "every client method the package calls exists on the double",
    ),
    Mutation(
        "double-accepts-any-transcript", 43, "mini_loop/fake_llm.py",
        '        validate_request(kwargs)\n        if self._parent.nonstreaming_ceiling is not None:',
        "        if self._parent.nonstreaming_ceiling is not None:",
        "tests/test_transcript_contract.py",
        "the double refuses the conversations the provider refuses",
    ),
    Mutation(
        "continuation-orphans-a-tool-call", 43, "mini_loop/recovery.py",
        "            if (getattr(resp, \"stop_reason\", None) == \"max_tokens\"\n                    and truncated_with_tools):",
        "            if False:",
        "tests/test_transcript_contract.py",
        "a truncated chunk holding a tool call is executed, not continued",
    ),
    Mutation(
        "double-allows-unsigned-thinking", 44, "mini_loop/fake_llm.py",
        '            if block_field(block, "type") == "thinking" and not block_field(\n                block, "signature"\n            ):',
        "            if False:",
        "tests/test_strictest_provider.py",
        "a thinking block must carry back its signature",
    ),
    Mutation(
        "double-allows-extra-breakpoints", 44, "mini_loop/fake_llm.py",
        "    if breakpoints > MAX_CACHE_BREAKPOINTS:",
        "    if False:",
        "tests/test_strictest_provider.py",
        "no more cache_control blocks than the provider allows",
    ),
    Mutation(
        "cache-budget-drifts-from-the-limit", 44, "mini_loop/caching.py",
        "MAX_BREAKPOINTS = 4",
        "MAX_BREAKPOINTS = 6",
        "tests/test_strictest_provider.py",
        "the policy's budget and the provider's ceiling stay the same number",
    ),
    Mutation(
        "skills-shadow-silently", 45, "mini_loop/skills.py",
        "            if name in self.skills:",
        "            if False:",
        "tests/test_skill_loading.py",
        "a planted skill cannot take over an established name",
    ),
    Mutation(
        "skill-name-unvalidated", 45, "mini_loop/skills.py",
        "            if not SKILL_NAME.match(name):",
        "            if False:",
        "tests/test_skill_loading.py",
        "a skill name cannot break out of the wrapper the model reads",
    ),
    Mutation(
        "skill-body-uncapped", 45, "mini_loop/skills.py",
        "            if len(body) > MAX_SKILL_BODY:",
        "            if False:",
        "tests/test_skill_loading.py",
        "a skill body cannot inject half a million tokens",
    ),
    Mutation(
        "memory-written-unmasked", 46, "mini_loop/memory.py",
        "            if self.secrets is not None:\n                text = self.secrets.mask(text)",
        "            pass",
        "tests/test_memory_hygiene.py",
        "a memory cannot carry a credential to disk",
    ),
    Mutation(
        "memory-index-uncapped", 46, "mini_loop/memory.py",
        "            if len(rendered) > MAX_INDEX:",
        "            if False:",
        "tests/test_memory_hygiene.py",
        "the index fed into every request is bounded",
    ),
    Mutation(
        "memory-slug-uncapped", 46, "mini_loop/memory.py",
        '[:MAX_SLUG]',
        "",
        "tests/test_memory_hygiene.py",
        "a long memory name does not crash the tool",
    ),
    Mutation(
        "cron-fires-into-nothing-silently", 47, "mini_loop/cron.py",
        "        if session is None:",
        "        if False:",
        "tests/test_cron_hygiene.py",
        "a job whose session is gone reports the lost occurrence",
    ),
    Mutation(
        "cron-fires-with-human-authority", 127, "mini_loop/cron.py",
        "            session.run(prompt, run_context=RunContext.default())",
        "            session.run(prompt, run_context=RunContext.explicit_human())",
        "tests/test_cron_authority.py::test_a_cron_fired_turn_runs_untrusted_not_as_the_human",
        "a durable, unattended cron prompt fires with UNTRUSTED authority, never "
        "the human's: human authority would let a scheduled turn launch workflows "
        "on every firing, which require EXPLICIT_HUMAN",
    ),
    Mutation(
        "cron-prompt-stored-raw", 47, "mini_loop/cron.py",
        "        if self.secrets is not None:\n            for record in durable:",
        "        if False:\n            for record in durable:",
        "tests/test_cron_hygiene.py",
        "a scheduled prompt cannot carry a credential to disk",
    ),
    Mutation(
        "cron-prompt-unbounded", 47, "mini_loop/cron.py",
        "        if len(prompt) > self.MAX_PROMPT:",
        "        if False:",
        "tests/test_cron_hygiene.py",
        "an oversized scheduled prompt is refused",
    ),
    Mutation(
        "events-persisted-unmasked", 48, "mini_loop/session.py",
        "            event = secrets.mask_payload(_json_safe(event))",
        "            pass",
        "tests/test_write_sites.py",
        "an event carrying a credential is masked before it is stored",
    ),
    Mutation(
        "mcp-separator-ambiguous", 49, "mini_loop/mcp.py",
        're.sub(r"_+", "_", re.sub(r"[^a-zA-Z0-9_-]", "_", name)).strip("_") or "unnamed"',
        're.sub(r"[^a-zA-Z0-9_-]", "_", name)',
        "tests/test_mcp_boundary.py",
        "a name component cannot contain the registration separator",
    ),
    Mutation(
        "mcp-server-can-take-over", 49, "mini_loop/mcp.py",
        "        if owner is not None and owner != client.name:",
        "        if False:",
        "tests/test_mcp_boundary.py",
        "one server's tool cannot be replaced by another's",
    ),
    Mutation(
        "mcp-call-unbounded", 49, "mini_loop/mcp.py",
        "                    return await asyncio.wait_for(\n                        c.call_tool(orig, kwargs), timeout=seconds\n                    )",
        "                    return await c.call_tool(orig, kwargs)",
        "tests/test_mcp_boundary.py",
        "a hung MCP server cannot hold the turn open",
    ),
    Mutation(
        "peer-message-unbounded", 50, "mini_loop/teams.py",
        "        if len(content) > self.MAX_CONTENT:",
        "        if False:",
        "tests/test_content_stores.py",
        "a peer cannot deliver an unbounded message into another agent's context",
    ),
    Mutation(
        "peer-message-unmasked", 50, "mini_loop/teams.py",
        "                payload = (\n                    self.secrets.mask_payload(msg) if self.secrets is not None else msg\n                )",
        "                payload = msg",
        "tests/test_content_stores.py",
        "a message carrying a credential is masked before it is stored",
    ),
    Mutation(
        "peer-message-masked-after-serialize", 120, "mini_loop/teams.py",
        "                payload = (\n                    self.secrets.mask_payload(msg) if self.secrets is not None else msg\n                )\n                with path.open(\"a\") as stream:\n                    stream.write(json.dumps(payload) + \"\\n\")",
        "                line = json.dumps(msg)\n                if self.secrets is not None:\n                    line = self.secrets.mask(line)\n                with path.open(\"a\") as stream:\n                    stream.write(line + \"\\n\")",
        "tests/test_content_stores.py::test_a_credential_does_not_reach_disk",
        "the mailbox masks the message structure before json escapes it, so a "
        "non-ASCII credential does not survive into a peer's inbox",
    ),
    Mutation(
        "memory-body-uncapped", 50, "mini_loop/memory.py",
        "            if len(body) > MAX_BODY:",
        "            if False:",
        "tests/test_content_stores.py",
        "a memory body is bounded, not only its index",
    ),
    Mutation(
        "todo-field-uncapped", 133, "mini_loop/agent.py",
        '            if len(content) > MAX_TODO_FIELD:\n                content = content[:MAX_TODO_FIELD] + " [truncated]"',
        "            pass",
        "tests/test_agent.py::test_todo_fields_are_size_bounded",
        "a todo's text is size-bounded, not only the count: the board renders "
        "into runtime_facts and re-injects on every change, so an uncapped field "
        "floods the context each edit",
    ),
    Mutation(
        "task-field-unmasked", 50, "mini_loop/tasks.py",
        "        if self.secrets is not None:\n            payload = self.secrets.mask_payload(payload)",
        "        pass",
        "tests/test_content_stores.py",
        "task instructions cannot carry a credential to disk",
    ),
    Mutation(
        "task-masked-after-serialize", 120, "mini_loop/tasks.py",
        "        if self.secrets is not None:\n            payload = self.secrets.mask_payload(payload)\n        atomic_write_text(target, json.dumps(payload, indent=2))",
        "        text = json.dumps(payload, indent=2)\n        if self.secrets is not None:\n            text = self.secrets.mask(text)\n        atomic_write_text(target, text)",
        "tests/test_content_stores.py::test_a_credential_does_not_reach_disk",
        "the task board masks the record structure before json escapes it, so a "
        "non-ASCII credential does not survive to disk",
    ),
    Mutation(
        "problem-log-unbounded", 51, "mini_loop/problems.py",
        "        if text in self.counts:\n            self.counts[text] += 1\n            return",
        "        pass",
        "tests/test_problem_log.py",
        "a repeated fault is counted, not accumulated",
    ),
    Mutation(
        "problem-log-unlimited", 51, "mini_loop/problems.py",
        "        if len(self) >= self.limit:",
        "        if False:",
        "tests/test_problem_log.py",
        "the error channel keeps a bounded number of distinct problems",
    ),
    Mutation(
        "audit-drops-the-counts", 51, "mini_loop/audit.py",
        '    scheduled = getattr(getattr(manager, "cron", None), "problems", [])',
        '    scheduled = list(getattr(getattr(manager, "cron", None), "problems", []))',
        "tests/test_problem_log.py",
        "the audit shows how often a problem recurred",
    ),
    Mutation(
        "memory-reparses-everything", 52, "mini_loop/memory.py",
        "            cached = self._parsed.get(path.name)\n            if cached is not None and cached[0] == key:\n                return cached[1]",
        "            pass",
        "tests/test_memory_scaling.py",
        "an unchanged memory is not re-parsed on every turn",
    ),
    Mutation(
        "memory-rebuilds-index-per-write", 52, "mini_loop/memory.py",
        "            self._index_dirty = True",
        "            self._rebuild_index()",
        "tests/test_memory_scaling.py",
        "the index is rebuilt on read, not on every write",
    ),
    Mutation(
        "memory-cache-goes-stale", 52, "mini_loop/memory.py",
        "            key = (stat.st_mtime_ns, stat.st_size)",
        "            key = None if False else (0, 0)",
        "tests/test_memory_scaling.py",
        "a memory changed on disk is re-read, not served from cache",
    ),
    Mutation(
        "double-counts-char-by-char", 53, "mini_loop/fake_llm.py",
        'ascii_chars = len(payload.encode("ascii", "ignore"))',
        "ascii_chars = sum(1 for char in payload if ord(char) < 128)",
        "tests/test_double_cost.py",
        "the offline model's token counter is not the slowest thing in a turn",
    ),
    Mutation(
        "double-miscounts-wide-text", 53, "mini_loop/fake_llm.py",
        "    return int(ascii_chars / 4 + wide_chars) + 8",
        "    return int(len(payload) / 4) + 8",
        "tests/test_double_cost.py",
        "the double charges non-ASCII more, so metering tests mean something",
    ),
    Mutation(
        "occurrences-lost-on-eviction", 54, "mini_loop/problems.py",
        "        return self._total",
        "        return sum(self.counts.values())",
        "tests/test_problem_log_eviction.py",
        "every occurrence is counted, including evicted ones",
    ),
    Mutation(
        "churn-not-reported", 54, "mini_loop/problems.py",
        "        return self.dropped > self.limit",
        "        return False",
        "tests/test_problem_log_eviction.py",
        "a log too small for its subsystem says its counts are lower bounds",
    ),
    Mutation(
        "broadcast-ignores-refusals", 55, "mini_loop/teams.py",
        '            if str(result).startswith("Error:"):\n                refused.append(f"{teammate}: {result}")\n            else:\n                sent += 1',
        "            sent += 1",
        "tests/test_team_tools.py",
        "a broadcast the bus refused is not reported as delivered",
    ),
    Mutation(
        "shutdown-authorization-dropped", 55, "mini_loop/teams.py",
        # The lead-only guard is spelled identically in request_plan and
        # review_plan; the error message is what makes the shutdown site unique.
        '        if ctx.state.get("agent_name", "lead") != "lead":\n'
        '            return "Error: only the lead can request teammate shutdown"',
        '        if False:\n'
        '            return "Error: only the lead can request teammate shutdown"',
        "tests/test_team_tools.py",
        "only the lead may request a teammate shutdown",
    ),
    Mutation(
        "worktree-removal-always-forces", 56, "mini_loop/worktrees.py",
        '        remove_args = ["worktree", "remove", str(path)]\n        if discard_changes:\n            remove_args.append("--force")',
        '        remove_args = ["worktree", "remove", str(path), "--force"]',
        "tests/test_worktree_safety.py",
        "git's own check backs up the harness instead of being overruled",
    ),
    Mutation(
        "worktree-keeps-nothing-unverified", 56, "mini_loop/worktrees.py",
        "        if not discard_changes and files < 0:",
        "        if False:",
        "tests/test_worktree_safety.py",
        "a worktree whose state cannot be read is kept, not removed",
    ),
    Mutation(
        "delivery-drops-oversized-work", 57, "mini_loop/manager.py",
        "        if len(content) > limit:",
        "        if False:",
        "tests/test_discarded_results.py",
        "a teammate's finished work reaches the lead instead of vanishing",
    ),
    Mutation(
        "delivery-failure-unrecorded", 57, "mini_loop/manager.py",
        '        if str(result).startswith("Error:"):',
        "        if False:",
        "tests/test_discarded_results.py",
        "a refused delivery is recorded rather than discarded",
    ),
    Mutation(
        "background-skips-the-sandbox", 58, "mini_loop/background.py",
        "                *self.sandbox.argv(command), cwd=str(self.workspace),\n                env=environment,",
        "                \"/bin/sh\", \"-c\", command, cwd=str(self.workspace),\n                env=environment,",
        "tests/test_background_parity.py",
        "a background command is confined exactly as a foreground one is",
    ),
    Mutation(
        "background-inherits-the-environment", 58, "mini_loop/background.py",
        "            environment = self.secrets.scrub_env(os.environ)",
        "            environment = dict(os.environ)",
        "tests/test_background_parity.py",
        "a background command cannot read credentials it did not name",
    ),
    Mutation(
        "background-result-unmasked", 58, "mini_loop/background.py",
        "                result = self.secrets.mask(\n                    (out or b\"\").decode(\"utf-8\", \"replace\").strip()\n                )[:OUTPUT_CAP] or \"",
        "                result = (out or b\"\").decode(\"utf-8\", \"replace\").strip()[:OUTPUT_CAP] or \"",
        "tests/test_background_parity.py",
        "a background result is masked before it is stored and injected",
    ),
    Mutation(
        "mcp-server-inherits-credentials", 59, "mini_loop/mcp.py",
        "                *self.command, env=self._environment(),",
        "                *self.command,",
        "tests/test_spawn_sites.py",
        "an MCP server does not inherit every credential in the environment",
    ),
    Mutation(
        "mcp-startup-unbounded", 59, "mini_loop/mcp.py",
        "                await asyncio.wait_for(self._handshake(), timeout=self.timeout)",
        "                await self._handshake()",
        "tests/test_spawn_sites.py",
        "an unresponsive MCP server cannot hang registration",
    ),
    Mutation(
        "mcp-line-limit-default", 60, "mini_loop/mcp.py",
        "                *self.command, env=self._environment(), limit=MAX_RPC_LINE,",
        "                *self.command, env=self._environment(),",
        "tests/test_mcp_message_size.py",
        "a tool result larger than 64 KiB arrives instead of raising",
    ),
    Mutation(
        "mcp-result-uncapped", 60, "mini_loop/mcp.py",
        "        if len(rendered) > MAX_TOOL_RESULT:",
        "        if False:",
        "tests/test_mcp_message_size.py",
        "an unbounded tool result cannot become the whole context",
    ),
    Mutation(
        "read-loads-the-whole-file", 140, "mini_loop/tools.py",
        "                data = \"\" if hit_eof else handle.read(READ_CHAR_CAP + 1)",
        "                data = \"\" if hit_eof else handle.read()",
        "tests/test_output_truncation.py::test_a_read_does_not_load_a_huge_file_into_memory",
        "read_file bounds what it pulls into memory: reading a huge file whole "
        "OOMs the process (all tenants), even though the output is capped",
    ),
    Mutation(
        "edit-loads-a-huge-file-whole", 141, "mini_loop/tools.py",
        "            if size > READ_CHAR_CAP:",
        "            if False:",
        "tests/test_output_truncation.py::test_an_edit_refuses_a_huge_file_instead_of_loading_it",
        "edit_file refuses a file too large to load rather than OOMing on it: "
        "the read-cap of round 140 cannot apply, since the edit needs the whole "
        "content, so the size is checked before the read",
    ),
    Mutation(
        "summary-reads-the-whole-body", 143, "mini_loop/trajectory.py",
        "            for line in handle:\n"
        "                try:\n"
        "                    record = json.loads(line)",
        "            for line in handle.readlines():\n"
        "                try:\n"
        "                    record = json.loads(line)",
        "tests/test_trajectory.py::test_a_listing_does_not_read_the_event_bodies_it_discards",
        "a listing summary streams one record at a time: materialising the whole "
        "event body to summarise it makes list()/count() cost O(recorded content) "
        "and lets one tenant's oversized trajectory inflate everyone's listing",
    ),
    Mutation(
        "mailbox-read-loads-whole-file", 144, "mini_loop/teams.py",
        "        if size <= self.MAX_READ_BYTES:",
        "        if True:",
        "tests/test_content_stores.py::test_reading_an_undrained_mailbox_does_not_load_the_whole_file",
        "a mailbox read is bounded to its delivered tail: an undrained mailbox "
        "grows without bound, and reading it whole OOMs the shared process even "
        "though the delivered batch is capped at MAX_INBOX",
    ),
    Mutation(
        "in-memory-mailbox-uncapped", 144, "mini_loop/teams.py",
        "                return messages[-self.MAX_INBOX:]",
        "                return messages",
        "tests/test_content_stores.py::test_the_in_memory_mailbox_is_bounded_like_the_persisted_one",
        "the MAX_INBOX bound belongs to the mailbox, not the backend: the "
        "in-memory path must cap the delivered batch like the persisted one",
    ),
    Mutation(
        "retry-after-honored-unbounded", 145, "mini_loop/recovery.py",
        "        return max(0.0, min(retry_after, MAX_RETRY_AFTER_MS / 1000.0))",
        "        return retry_after",
        "tests/test_recovery_backoff.py::test_an_absurd_retry_after_is_clamped_not_honored",
        "a server's Retry-After is honored only up to a finite ceiling: passing "
        "it through verbatim lets one response header sleep the turn for hours, "
        "and `Retry-After: inf` forever",
    ),
    Mutation(
        "retry-after-nonfinite-accepted", 145, "mini_loop/recovery.py",
        "        if seconds is None or not math.isfinite(seconds) or seconds < 0:",
        "        if seconds is None:",
        "tests/test_recovery_backoff.py::test_a_malformed_retry_after_falls_back_to_bounded_backoff",
        "a malformed Retry-After (inf/nan/negative) is not a delay: it is "
        "rejected at the parse boundary so it cannot become an infinite sleep",
    ),
    Mutation(
        "background-results-never-shed", 146, "mini_loop/background.py",
        "        while len(self._finished) > self.max_results_retained:",
        "        while False:",
        "tests/test_background_parity.py::test_finished_task_results_do_not_accumulate_without_bound",
        "a completed background result is released beyond the retention bound: "
        "`_tasks` kept every result forever, leaking memory with each "
        "background_run the way the action journal did before it was bounded",
    ),
    Mutation(
        "background-settle-not-called", 146, "mini_loop/background.py",
        "        self._settle(bg_id)",
        "        pass",
        "tests/test_background_parity.py::test_finished_task_results_do_not_accumulate_without_bound",
        "every finished task is recorded for shedding: skipping it leaves the "
        "retention queue empty so nothing is ever released",
    ),
    Mutation(
        "protocols-never-pruned", 147, "mini_loop/manager.py",
        "        self._prune_protocols()\n        return state",
        "        return state",
        "tests/test_team_tools.py::test_resolved_protocol_handshakes_do_not_accumulate",
        "team protocol handshakes are bounded: `self.protocols` was only ever "
        "added to, so every plan/shutdown request leaked a ProtocolState forever "
        "the way the background result store did before round 146",
    ),
    Mutation(
        "protocols-evict-live-pending", 147, "mini_loop/manager.py",
        '        resolved = [rid for rid, s in self.protocols.items() if s.status != "pending"]',
        "        resolved = list(self.protocols)",
        "tests/test_team_tools.py::test_resolved_protocol_handshakes_do_not_accumulate",
        "resolved handshakes are evicted before pending ones: a pending request "
        "is a live handshake awaiting a response, and dropping it reads as "
        "'never asked' -- the action journal's rule",
    ),
    Mutation(
        "missing-task-dependency-hidden", 148, "mini_loop/tasks.py",
        "                missing = [dep for dep in t.blockedBy if dep not in existing]",
        "                missing = []",
        "tests/test_features.py::test_a_task_blocked_by_a_missing_dependency_is_surfaced",
        "a task blocked by a dependency that does not exist is permanently "
        "unrunnable, and the task list must say so rather than showing it as an "
        "ordinary blocked task -- the round-49 'does a failure report?' channel",
    ),
    Mutation(
        "workflow-terminal-runs-unbounded", 149, "mini_loop/workflows/store.py",
        "            for run in terminal[: len(terminal) - keep]:",
        "            for run in []:",
        "tests/test_workflows.py::test_terminal_runs_are_bounded_but_active_and_unread_are_spared",
        "the shared workflow store bounds retained terminal runs: it is created "
        "once at the manager and only ever added to, so a completed run's whole "
        "state graph leaked forever (rounds 146/147's class, third instance)",
    ),
    Mutation(
        "workflow-prune-drops-unread-result", 149, "mini_loop/workflows/store.py",
        "                    if run.is_terminal and run.run_id not in undelivered",
        "                    if run.is_terminal",
        "tests/test_workflows.py::test_terminal_runs_are_bounded_but_active_and_unread_are_spared",
        "a terminal run with an undelivered outbox message is an unread result, a "
        "live commitment spared from eviction like a pending handshake (round 147)",
    ),
    Mutation(
        "request-content-length-unchecked", 150, "mini_loop/server.py",
        "                    if int(value) > self.max_bytes:",
        "                    if False:",
        "tests/test_request_size.py::test_an_oversized_body_is_refused_without_being_buffered",
        "an oversized body is refused from its Content-Length before it is "
        "buffered: reading a multi-gigabyte body to parse it OOMs the shared "
        "server, every tenant on it with it",
    ),
    Mutation(
        "request-streamed-bytes-uncounted", 150, "mini_loop/server.py",
        "            if total > self.max_bytes:",
        "            if False:",
        "tests/test_request_size.py::test_a_chunked_body_over_the_cap_is_still_refused",
        "the streamed body is counted too, so a chunked request or a lying "
        "Content-Length cannot slip a large body past the header check",
    ),
    Mutation(
        "trajectory-export-readable-by-anyone", 151, "mini_loop/server.py",
        "            await _owned_trajectory_summary(request, store, trajectory_id)\n"
        "            disposition = (",
        "            disposition = (",
        "tests/test_trajectory_ownership.py",
        "the export route is owner-scoped like the inspect route: a stranger who "
        "knows a trajectory id gets 404, not another tenant's recorded conversation",
    ),
    Mutation(
        "trajectory-json-export-unbounded", 151, "mini_loop/server.py",
        '            if size > MAX_TRAJECTORY_JSON_BYTES:\n'
        '                raise HTTPException(\n'
        '                    status_code=413,\n'
        '                    detail=(\n'
        '                        f"trajectory is {size:,} bytes; too large to export as one "',
        '            if False:\n'
        '                raise HTTPException(\n'
        '                    status_code=413,\n'
        '                    detail=(\n'
        '                        f"trajectory is {size:,} bytes; too large to export as one "',
        "tests/test_trajectory_ownership.py::test_a_trajectory_too_large_for_one_json_document_is_refused",
        "a trajectory too large to build as one JSON document is refused with a "
        "pointer to the streaming JSONL export: a long run stores the full model "
        "input at every call, so building it whole OOMs the shared server",
    ),
    Mutation(
        "trajectory-json-inspect-unbounded", 151, "mini_loop/server.py",
        '            if size > MAX_TRAJECTORY_JSON_BYTES:\n'
        '                raise HTTPException(\n'
        '                    status_code=413,\n'
        '                    detail=(\n'
        '                        f"trajectory is {size:,} bytes; too large to render as one "',
        '            if False:\n'
        '                raise HTTPException(\n'
        '                    status_code=413,\n'
        '                    detail=(\n'
        '                        f"trajectory is {size:,} bytes; too large to render as one "',
        "tests/test_trajectory_ownership.py::test_a_trajectory_too_large_for_one_json_document_is_refused",
        "the inspect route bounds the same JSON build as the export route: both "
        "read the whole trajectory into memory, so both refuse an oversized one",
    ),
    Mutation(
        "bash-reads-all-output-into-memory", 169, "mini_loop/tools.py",
        "            chunk = stream.read(min(65536, room) if room > 0 else 1)",
        "            chunk = stream.read()",
        "tests/test_output_truncation.py::test_bash_output_is_memory_bounded_not_just_capped",
        "run_bash drains stdout with a byte bound so a high-output command cannot "
        "fill memory before the output is capped: reading it all in one call is "
        "the round-140 OOM (bounded output, unbounded work) for bash",
    ),
    Mutation(
        "read-offset-cannot-page-past-the-cap", 168, "mini_loop/tools.py",
        "                for _ in range(offset):",
        "                for _ in range(0):",
        "tests/test_output_truncation.py::test_read_offset_pages_past_the_char_cap",
        "read_file's offset skips lines from the file so it reaches content past "
        "the READ_CHAR_CAP window: capping first and offsetting within the window "
        "left every line beyond the cap unreachable despite the notice's advice",
    ),
    Mutation(
        "read-offset-skip-loads-an-overlong-line", 168, "mini_loop/tools.py",
        "                        piece = handle.readline(READ_CHAR_CAP)",
        "                        piece = handle.readline()",
        "tests/test_output_truncation.py::test_read_offset_pages_past_the_char_cap",
        "the offset skip reads each line in READ_CHAR_CAP-sized pieces so an "
        "overlong line cannot breach the read's memory bound during the skip",
    ),
    Mutation(
        "glob-truncation-notice-sorts-to-the-top", 167, "mini_loop/tools.py",
        "            lines = sorted(set(matches))\n            if truncated:",
        "            if truncated:\n                matches.append(marker)\n            lines = sorted(set(matches))\n            if False:",
        "tests/test_output_truncation.py::test_a_large_glob_says_it_was_cut",
        "the glob truncation notice is appended after the sort so it trails the "
        "list: sorted in with the matches its leading '.' sorts it to the top, "
        "reading as if nothing matched",
    ),
    Mutation(
        "in-memory-inbox-grows-unbounded", 166, "mini_loop/teams.py",
        "                if len(inbox) > self.MAX_INBOX:",
        "                if False:",
        "tests/test_content_stores.py::test_the_in_memory_mailbox_bounds_the_queue_not_only_the_read",
        "the in-memory inbox sheds oldest on send so the held queue is bounded: "
        "capping only what read returns let the queue hold every message ever "
        "sent to a recipient that never drains, unbounded in RAM",
    ),
    Mutation(
        "task-board-shows-every-row", 165, "mini_loop/tasks.py",
        "        if len(lines) > MAX_TASK_BOARD:",
        "        if False:",
        "tests/test_features.py::test_the_task_board_is_bounded_on_both_axes",
        "the task board caps its row count: tasks are never deleted, so an "
        "uncapped board renders one line per task ever created -- an unbounded "
        "tool output after a long multi-agent session",
    ),
    Mutation(
        "task-board-shows-full-subjects", 165, "mini_loop/tasks.py",
        "            if len(subject) > MAX_SUBJECT_DISPLAY:",
        "            if False:",
        "tests/test_features.py::test_the_task_board_is_bounded_on_both_axes",
        "the task board previews each subject: a subject may be up to 16 KB, so "
        "rendering it in full puts a per-row unbounded field on the board even "
        "when the row count is capped",
    ),
    Mutation(
        "background-listing-is-unbounded", 164, "mini_loop/background.py",
        "        shown = items[-MAX_TASK_LISTING:]",
        "        shown = items",
        "tests/test_background_parity.py::test_the_task_listing_is_bounded_regardless_of_task_count",
        "check_background's no-id listing is capped to the most recent tasks: "
        "rendering one line per task ever run is an unbounded tool output that "
        "floods the model context after a long session of background work",
    ),
    Mutation(
        "write-file-is-not-atomic", 163, "mini_loop/tools.py",
        "            atomic_write_text(fp, content)",
        "            fp.write_text(content)",
        "tests/test_output_truncation.py::test_a_write_is_atomic_so_a_failure_leaves_the_original",
        "write_file replaces the target atomically (temp + rename): a bare "
        "write_text truncates in place, so a crash mid-write or a failed write "
        "leaves a half-written file a teammate sharing the workspace can read",
    ),
    Mutation(
        "ambiguous-edit-hits-the-first-match", 162, "mini_loop/tools.py",
        "            if occurrences > 1:",
        "            if False:",
        "tests/test_output_truncation.py::test_an_ambiguous_edit_is_refused_not_applied_to_the_first_match",
        "an edit whose old_text matches many places is refused, not silently "
        "applied to the first: replacing the first edits a location the model "
        "may not have meant and reports success, so it never learns",
    ),
    Mutation(
        "dead-mcp-server-never-restarts", 161, "mini_loop/mcp.py",
        "            if self._proc is not None and self._proc.returncode is None:",
        "            if self._proc is not None:",
        "tests/test_mcp_restart.py::test_a_dead_server_is_restarted_on_the_next_call",
        "a crashed MCP subprocess is restarted on the next call: without the "
        "returncode check the client reuses the dead process forever, so one "
        "transient crash of the least-trusted component bricks its tools",
    ),
    Mutation(
        "worktree-sandbox-cannot-commit", 160, "mini_loop/sandbox.py",
        "            if common is not None and common not in self.writable_roots:",
        "            if False:",
        "tests/test_sandbox.py::test_a_worktree_sandbox_can_write_the_shared_git_dir",
        "a git worktree's sandbox allows its shared .git dir: the harness "
        "provisions a worktree per session, and confining writes to the worktree "
        "alone breaks every git command that writes (index.lock is outside it)",
    ),
    Mutation(
        "sandbox-reads-every-credential", 152, "mini_loop/sandbox.py",
        "        if protect_credentials:\n"
        "            roots = [*default_unreadable_roots(), *roots]",
        "        if False:\n"
        "            roots = [*default_unreadable_roots(), *roots]",
        "tests/test_sandbox.py::test_default_sandbox_denies_the_hosts_credential_stores",
        "the default sandbox denies the host's credential stores: Seatbelt "
        "confines writes but allows reads broadly, so without this a confined "
        "shell reads ~/.ssh and copies it into the agent-readable workspace",
    ),
    Mutation(
        "mask-payload-skips-dict-keys", 154, "mini_loop/secrets.py",
        "                self.mask(key): self.mask_payload(item)",
        "                key: self.mask_payload(item)",
        "tests/test_secrets.py::test_mask_payload_masks_dict_keys_not_only_values",
        "mask_payload masks dict keys, not only values: a credential-listing tool "
        "that keys a map by the credential otherwise writes the secret into the "
        "trajectory and durable tables as an unmasked key",
    ),
    Mutation(
        "explore-subagent-not-read-only", 155, "mini_loop/subagents.py",
        '            state["permission_mode"] = "readonly"',
        '            state["permission_mode"] = "interactive"',
        "tests/test_extension_contracts.py::test_an_explore_subagent_is_read_only",
        "an Explore subagent runs read-only: the task tool promises the model it "
        "is, but interactive mode runs a plain `echo x > file` via bash with no "
        "approval, so the promise has to be enforced by the permission gate",
    ),
    Mutation(
        "explore-registry-offers-bash", 155, "mini_loop/tool_policy.py",
        '        "repo.references",\n    }\n)\n\nWORKER_CAPABILITIES',
        '        "repo.references",\n        "process.exec",\n    }\n)\n\nWORKER_CAPABILITIES',
        "tests/test_role_tool_policy.py::test_explore_inherits_semantic_reads_without_write_or_exec",
        "the read-only explorer capability policy offers no process.exec/bash: "
        "offering a tool read-only mode denies is a capability the model is told "
        "it has and cannot use",
    ),
    Mutation(
        "worktree-switch-leaves-background-misconfined", 156, "mini_loop/agent.py",
        "            background.sandbox = self.sandbox.for_workspace(self.workspace)",
        "            pass",
        "tests/test_background_parity.py::test_entering_a_worktree_re_confines_background_too",
        "entering a worktree re-confines background_run too, not just its cwd: "
        "run_bash's sandbox is re-bound by the new Toolset, and its background "
        "sibling must move with it or it writes the workspace it left, not its own",
    ),
    Mutation(
        "lease-claim-result-discarded", 158, "mini_loop/manager.py",
        "        ):\n            session.lease_confirmed = True",
        "        ):\n            pass",
        "tests/test_leases.py::test_a_successful_claim_confirms_the_lease_a_lost_one_does_not",
        "a successful lease claim records that the lease is held, so a mid-turn "
        "loss is a real loss on every run path -- the sibling to `_renew_lease` "
        "checking its renewal (round 157), the last discarded control-plane result",
    ),
    Mutation(
        "lease-renewal-loss-silently-ignored", 157, "mini_loop/session.py",
        "            if self.lease_confirmed:",
        "            if False:",
        "tests/test_leases.py::test_a_held_lease_lost_mid_turn_stops_the_session",
        "a held lease lost mid-turn stops the session: renewal per persistence "
        "beat discarded its result, so a lapse under a long event-quiet operation "
        "let another process take the session while this one kept driving it",
    ),
    Mutation(
        "lease-confirmation-not-recorded", 157, "mini_loop/session.py",
        "        # We hold it now; a later renewal failure is a real loss, not a claim\n"
        "        # that never won.\n"
        "        self.lease_confirmed = True",
        "        pass",
        "tests/test_leases.py::test_a_held_lease_lost_mid_turn_stops_the_session",
        "holding the lease is recorded, so a later renewal failure can tell a real "
        "loss from a claim that never won -- without it every lost claim looks the same",
    ),
    Mutation(
        "self-delete-reads-as-a-lease-steal", 157, "mini_loop/manager.py",
        "        session.lease_owner = None\n        session.lease_confirmed = False",
        "        pass",
        "tests/test_durable_approvals.py::test_a_session_delete_is_recorded_as_cancelled",
        "deleting a session disowns its lease before waking a parked turn, so the "
        "self-delete is not read as another process stealing the lease mid-cancellation",
    ),
    Mutation(
        "sse-resume-gaps-beyond-the-backlog", 159, "mini_loop/server.py",
        "                if last_seen > 0 and loader is not None:",
        "                if False:",
        "tests/test_event_stream.py::test_a_resume_catches_up_from_the_store_beyond_the_backlog",
        "an SSE resume catches up from the durable store, not just the 200-event "
        "in-memory backlog: a client that missed more than the backlog otherwise "
        "gapped though the events were stored",
    ),
    Mutation(
        "index-shown-without-the-tool", 61, "mini_loop/prompts.py",
        'if memory is not None and memory.list() and "recall" in agent.tools:',
        "if memory is not None and memory.list():",
        "tests/test_context_matches_tools.py",
        "the memory index is shown only when the agent can act on it",
    ),
    Mutation(
        "confinement-not-stated", 62, "mini_loop/prompts.py",
        '    if getattr(sandbox, "confined", False):',
        "    if False:",
        "tests/test_agent_knows_its_confinement.py",
        "a confined agent is told where its boundary is",
    ),
    Mutation(
        "confinement-claimed-falsely", 62, "mini_loop/sandbox.py",
        "class NullSandbox:\n    \"\"\"Run on the host with no confinement. Trusted callers only.\"\"\"\n\n    confined = False",
        "class NullSandbox:\n    \"\"\"Run on the host with no confinement. Trusted callers only.\"\"\"\n\n    confined = True",
        "tests/test_agent_knows_its_confinement.py",
        "an unconfined agent is not told it is confined",
    ),
    Mutation(
        "truncation-is-silent", 63, "mini_loop/tools.py",
        "    if len(text) <= OUTPUT_CAP:\n        return text",
        "    return text[:OUTPUT_CAP]",
        "tests/test_output_truncation.py",
        "output cut at the cap says so instead of ending mid-stream",
    ),
    Mutation(
        "command-output-loses-its-tail", 63, "mini_loop/tools.py",
        "        rendered = capped(out, keep_tail=True)",
        "        rendered = capped(out)",
        "tests/test_output_truncation.py",
        "a command's failure summary survives truncation",
    ),
    Mutation(
        "context-pressure-unreported", 64, "mini_loop/prompts.py",
        "    pressure = _context_pressure(agent)\n    if pressure:",
        "    pressure = \"\"\n    if pressure:",
        "tests/test_context_pressure.py",
        "an agent near its budget is told its history is being rewritten",
    ),
    Mutation(
        "context-pressure-unbucketed", 64, "mini_loop/prompts.py",
        "    for fraction, label in CONTEXT_BANDS:\n        if used >= threshold * fraction:",
        "    for fraction, label in [(0.0, f\"at {used * 100 // max(threshold, 1)}%\")]:\n        if used >= threshold * fraction:",
        "tests/test_context_pressure.py",
        "the value holds still across turns instead of injecting every turn",
    ),
    Mutation(
        "workflow-failure-not-notified", 69, "mini_loop/workflows/service.py",
        "            if current.is_terminal:\n                await self._enqueue_terminal_notification(current)",
        "            if False:\n                await self._enqueue_terminal_notification(current)",
        "tests/test_workflow_failure_path.py",
        "a workflow that fails tells the session that launched it",
    ),
    Mutation(
        "workflow-notification-batch-unbounded", 139, "mini_loop/workflows/store.py",
        "                if limit is not None and len(claimed) >= limit:\n                    break",
        "                pass",
        "tests/test_workflows.py::test_claim_outbox_caps_one_delivery_and_keeps_the_rest",
        "claim_outbox honours its limit: without it, a parent that launched many "
        "runs gets every result joined into one injected message, flooding the "
        "context (round 134's background-drain bound, for workflow results)",
    ),
    Mutation(
        "timeout-leaves-orphans", 70, "mini_loop/tools.py",
        "                start_new_session=True,\n            )\n            with self._live_lock:",
        "            )\n            with self._live_lock:",
        "tests/test_orphan_processes.py::test_foreground_command_requests_own_process_group",
        "a timed-out command's children are reaped, not left running",
    ),
    Mutation(
        "background-timeout-leaves-orphans", 70, "mini_loop/background.py",
        "            except asyncio.TimeoutError:\n                _kill_group(proc)",
        "            except asyncio.TimeoutError:\n                proc.kill()\n                await proc.wait()",
        "tests/test_orphan_processes.py",
        "a background command's children are reaped too",
    ),
    Mutation(
        "resource-gap-unreported", 71, "mini_loop/audit.py",
        '                "resource-limits",',
        '                "resource-limits-disabled",',
        "tests/test_resource_limits.py",
        "a shell deployment is told its consumption is unbounded",
    ),
    Mutation(
        "incomplete-registry-reads-as-safe", 124, "mini_loop/audit.py",
        "        if unregistered:",
        "        if False:",
        "tests/test_audit.py::test_an_incomplete_registry_flags_the_credentials_it_missed",
        "an existing but incomplete secret registry is flagged: a credential-"
        "shaped env var it missed is inherited by the shell and leaks unmasked, "
        "and 'has a registry' must not read as 'masks its credentials'",
    ),
    Mutation(
        "remote-audit-blind-to-incomplete-registry", 125, "mini_loop/audit.py",
        "    elif posture.get(\"secrets_unregistered\"):",
        "    elif False:",
        "tests/test_audit.py::test_a_remote_incomplete_registry_is_flagged_from_the_count",
        "a remote audit flags an incomplete registry from the count the server "
        "plumbs through its posture; without it the local fix leaves the remote "
        "path reading a clean bill of health while credentials leak",
    ),
    Mutation(
        "workflow-worker-has-no-readonly-backstop", 126, "mini_loop/workflows/runner.py",
        '            state={"permission_mode": "readonly"},',
        "            state=None,",
        "tests/test_workflows.py::test_the_worker_runs_readonly_with_a_permission_backstop",
        "the workflow worker runs in readonly mode so its permission hook denies "
        "a mutating tool; without it the read-only guarantee rests only on the "
        "tool allowlist, with nothing behind it",
    ),
    Mutation(
        "restore-skips-the-repair", 72, "mini_loop/session.py",
        "        repaired = self._close_unanswered_tools(self._expire_parked_approvals())\n        if repaired:\n            self._flush_messages()\n            self._unknown_tool_uses = tuple(repaired)",
        "        repaired = []",
        "tests/test_restart_continuity.py",
        "a restored session can make another request the provider accepts",
    ),
    Mutation(
        "trajectory-readable-by-anyone", 74, "mini_loop/server.py",
        # Anchored through the JSON-document wording: round 185's HTML view
        # route repeats the ownership-then-size prefix, and a two-line anchor
        # started matching both routes.
        "            await _owned_trajectory_summary(request, store, trajectory_id)\n"
        "            size = await asyncio.to_thread(store.byte_size, trajectory_id)\n"
        "            if size > MAX_TRAJECTORY_JSON_BYTES:\n"
        "                raise HTTPException(\n"
        "                    status_code=413,\n"
        "                    detail=(\n"
        '                        f"trajectory is {size:,} bytes; too large to render as one "',
        "            size = await asyncio.to_thread(store.byte_size, trajectory_id)\n"
        "            if size > MAX_TRAJECTORY_JSON_BYTES:\n"
        "                raise HTTPException(\n"
        "                    status_code=413,\n"
        "                    detail=(\n"
        '                        f"trajectory is {size:,} bytes; too large to render as one "',
        "tests/test_trajectory_ownership.py",
        "a recorded conversation is readable only by whoever made it (round 151: "
        "the check moved into `_owned_trajectory_summary`, which enforces it from "
        "the cheap header before any bulk read)",
    ),
    Mutation(
        "anyone-can-delete-a-session", 74, "mini_loop/server.py",
        "        _require(request, session_id)\n        if not _manager(request).delete(session_id):",
        "        if not _manager(request).delete(session_id):",
        "tests/test_trajectory_ownership.py",
        "only the owner may delete a session",
    ),
    Mutation(
        "recorded-owner-ignored", 75, "mini_loop/server.py",
        "    recorded = record.get(\"owner\")\n    if recorded is not None:",
        "    recorded = None\n    if recorded is not None:",
        "tests/test_trajectory_owner_durability.py",
        "the owner recorded on disk outlives the session and the process",
    ),
    Mutation(
        "owner-not-written", 75, "mini_loop/trajectory.py",
        '            "owner": owner,',
        '            "owner": None,',
        "tests/test_trajectory_owner_durability.py",
        "the trajectory record carries who made it",
    ),
    Mutation(
        "listing-gates-instead-of-filtering", 76, "mini_loop/server.py",
        "        return [r for r in records if _owns_trajectory(request, r, caller)]",
        "        return records",
        "tests/test_trajectory_listing_scope.py",
        "the listing filters by caller instead of gating on owning anything",
    ),
    Mutation(
        "collection-leaks-across-tenants", 77, "mini_loop/server.py",
        "        return [r for r in records if _owns_trajectory(request, r, caller)]",
        "        return records",
        "tests/test_two_tenant_isolation.py",
        "no collection endpoint shows one tenant another's identifiers",
    ),
    Mutation(
        "memory-not-scoped-to-owner", 78, "mini_loop/memory.py",
        "            if owner is None:\n                return found\n            return [m for m in found if m.get(\"owner\", \"anonymous\") == owner]",
        "            return found",
        "tests/test_memory_isolation.py",
        "one caller's memories stay out of another's context",
    ),
    Mutation(
        "runtime-facts-bypasses-the-seam", 78, "mini_loop/prompts.py",
        "    memory = memory_store_for(agent) if agent.state.get(\"memory\") is not None else None",
        '    memory = agent.state.get("memory")',
        "tests/test_memory_isolation.py",
        "the injected index goes through the owner-bound view",
    ),
    Mutation(
        "cron-cancel-unscoped", 79, "mini_loop/cron.py",
        "        if job is None or (session_id is not None and job.session_id != session_id):",
        "        if job is None:",
        "tests/test_cron_ownership.py",
        "one session cannot cancel another session's scheduled job",
    ),
    Mutation(
        "cron-tool-drops-the-session", 79, "mini_loop/cron.py",
        "        return sched.cancel(job_id, session_id=ctx.state.get(\"session_id\"))",
        "        return sched.cancel(job_id)",
        "tests/test_cron_ownership.py",
        "the tool passes its own session, so an agent cancels only its own",
    ),
    Mutation(
        # Same line as the entry above, aimed at a different test on purpose.
        # That one is hand-written for cron; this one asks whether the *generic*
        # sweep would have caught it with nobody having thought about cron at
        # all -- which is the only thing that helps with the sixth object.
        "cron-tool-drops-the-session-caught-generically", 80, "mini_loop/cron.py",
        "        return sched.cancel(job_id, session_id=ctx.state.get(\"session_id\"))",
        "        return sched.cancel(job_id)",
        "tests/test_tool_scoping.py::test_every_tool_taking_an_id_scopes_it",
        "an unscoped id in any agent-facing tool fails a test, without anyone "
        "having written a test for that particular tool",
    ),
    Mutation(
        "unreadable-memory-raises", 81, "mini_loop/memory.py",
        "        except (OSError, UnicodeDecodeError) as exc:",
        "        except (OSError,) as exc:",
        "tests/test_corrupt_stores.py",
        "one undecodable byte in the memory directory does not end every turn",
    ),
    Mutation(
        "unreadable-memory-unreported", 81, "mini_loop/memory.py",
        '            self.problems.append(f"unreadable memory {path.name}: {type(exc).__name__}")',
        "            pass",
        "tests/test_corrupt_stores.py::test_an_unreadable_memory_is_reported",
        "a skipped memory is reported rather than quietly dropped",
    ),
    Mutation(
        "corrupt-task-unreported", 81, "mini_loop/tasks.py",
        '            self.problems.append(f"unreadable task {p.name}: {type(exc).__name__}")',
        "            pass",
        "tests/test_corrupt_stores.py::test_a_corrupt_task_is_reported_rather_than_vanishing",
        "a task that will not parse is reported instead of reading as absent",
    ),
    Mutation(
        "durable-write-is-not-atomic", 82, "mini_loop/durable.py",
        "        os.replace(temporary, path)",
        "        path.write_bytes(payload)",
        "tests/test_durable_writes.py::test_a_failed_write_leaves_the_old_content",
        "a write that fails leaves the previous content, not an empty file",
    ),
    Mutation(
        "durable-write-is-not-flushed", 82, "mini_loop/durable.py",
        "            os.fsync(handle.fileno())\n"
        "        os.replace(temporary, path)",
        "            pass\n"
        "        os.replace(temporary, path)",
        "tests/test_durable_writes.py::test_the_write_is_actually_durable_not_just_renamed",
        "the temp file is on disk before the rename makes it the target",
    ),
    Mutation(
        "scratch-file-survives-an-interrupt", 82, "mini_loop/durable.py",
        "    except BaseException:\n"
        "        # Including KeyboardInterrupt and SystemExit:",
        "    except Exception:\n"
        "        # Including KeyboardInterrupt and SystemExit:",
        "tests/test_durable_writes.py::test_an_interrupt_leaves_no_scratch_file",
        "Ctrl-C during a write strands no .tmp for a later glob to find",
    ),
    Mutation(
        "shared-scratch-path", 82, "mini_loop/durable.py",
        '    temporary = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")',
        '    temporary = path.with_name(f".{path.name}.tmp")',
        "tests/test_durable_writes.py::test_concurrent_writers_do_not_share_a_scratch_path",
        "concurrent writers do not rename each other's half-written bytes",
    ),
    Mutation(
        "refactor-shrinks-the-write-inventory", 82, "tests/test_write_sites.py",
        '    "atomic_create_text",\n'
        '    "atomic_create_bytes",\n'
        '    "atomic_write_text",\n'
        '    "atomic_write_bytes",\n'
        '}',
        '}',
        "tests/test_write_sites.py::test_the_classification_has_no_dead_entries",
        "routing writes through a helper does not remove them from the masking "
        "inventory -- the refactor in this round did exactly that",
    ),
    Mutation(
        "block-access-scan-unanchored", 83, "tests/test_block_access.py",
        # The anchor as a whole, not one of its three assertions: nulling a
        # single line left the other two still failing on an empty scan, so
        # the module stayed anchored and the mutation rightly survived.
        "    modules = sorted(PACKAGE.rglob(\"*.py\"))",
        "    return\n    modules = sorted(PACKAGE.rglob(\"*.py\"))",
        "tests/test_scan_anchoring.py::test_every_scanning_module_is_anchored",
        "a module whose whole scanning surface goes green on an empty scan is "
        "reported -- this is round 82's shape, found by tooling not by reading",
    ),
    Mutation(
        "paused-turn-returned-as-final", 84, "mini_loop/agent.py",
        "            if not tool_blocks and reason in RESUMABLE_STOP_REASONS:",
        "            if False:",
        "tests/test_stop_reasons.py::test_a_paused_turn_is_resumed_not_returned",
        "a turn the model asked to continue is continued, not handed back as "
        "a finished answer",
    ),
    Mutation(
        "resumption-is-unbounded", 84, "mini_loop/agent.py",
        "                if self._resumptions <= MAX_RESUMPTIONS:",
        "                if True:",
        "tests/test_stop_reasons.py::test_resumption_is_bounded",
        "an always-pausing provider cannot loop the agent forever",
    ),
    Mutation(
        "refusal-returns-nothing", 84, "mini_loop/agent.py",
        "                    self.last_text = REFUSAL_NOTICE",
        "                    pass",
        "tests/test_stop_reasons.py::test_a_refusal_is_not_an_empty_answer",
        "a refusal is distinguishable from an empty answer",
    ),
    Mutation(
        "unknown-stop-reason-is-silent", 84, "mini_loop/agent.py",
        '                await self._send("provider_stop_unhandled", stop_reason=reason,\n'
        '                                 detail="unrecognized stop reason, treated as end of turn")',
        "                pass",
        "tests/test_stop_reasons.py::test_an_unrecognized_reason_is_reported_but_still_answers",
        "a stop reason the harness has never heard of is reported, not read as done",
    ),
    Mutation(
        "pause-budget-is-session-lifetime", 85, "mini_loop/agent.py",
        "        self._resumptions = 0\n        try:",
        "        try:",
        "tests/test_counter_scope.py::test_the_pause_budget_survives_a_long_session",
        "the pause budget is per turn, so a long session does not lose "
        "resumption permanently -- round 84 shipped it session-scoped",
    ),
    Mutation(
        "counter-scope-unclassified", 85, "tests/test_counter_scope.py",
        '    "_resumptions": "a pause budget that accrues across turns stops working",',
        "",
        "tests/test_counter_scope.py::test_every_counter_declares_its_scope",
        "a loop counter that declares no scope fails, so the next one cannot "
        "get session lifetime by being declared beside the others",
    ),
    Mutation(
        "action-results-never-released", 86, "mini_loop/actions.py",
        "                self._completed.append(action_id)\n                self._shed_old_results()",
        "                self._completed.append(action_id)",
        "tests/test_long_session_growth.py::test_results_are_released_beyond_the_bound",
        "a long-lived session does not hold every tool result forever "
        "(20,000 actions measured 81 MB)",
    ),
    Mutation(
        "subscriber-queue-unbounded", 131, "mini_loop/session.py",
        "        q: asyncio.Queue = asyncio.Queue(maxsize=SUBSCRIBER_QUEUE_MAX)",
        "        q: asyncio.Queue = asyncio.Queue()",
        "tests/test_long_session_growth.py::test_a_stalled_subscriber_queue_is_bounded",
        "a subscriber queue is bounded: a stalled SSE client otherwise "
        "accumulates every event for the life of the session, growing memory "
        "without limit while the backlog it could resume from stays capped",
    ),
    Mutation(
        "background-notification-batch-unbounded", 134, "mini_loop/background.py",
        "    dropped = max(0, len(done) - MAX_NOTIFICATIONS)\n    shown = done[-MAX_NOTIFICATIONS:] if dropped else done  # keep the newest",
        "    dropped = 0\n    shown = done",
        "tests/test_long_session_growth.py::test_the_background_notification_batch_is_bounded",
        "the background result batch is capped: a long round finishing many tasks "
        "otherwise drains them all into one injection and floods the context",
    ),
    Mutation(
        "shedding-evicts-the-record", 86, "mini_loop/actions.py",
        "            self._records[action_id] = replace(record, result=SHED_RESULT)",
        "            self._records.pop(action_id, None)",
        "tests/test_long_session_growth.py::test_a_shed_action_is_not_re_run",
        "reclaiming memory never makes a completed action read as never "
        "started, which would run its side effect twice",
    ),
    Mutation(
        "concurrent-turns-interleave", 87, "mini_loop/agent.py",
        "        async with self._turn_lock:\n            try:",
        "        if True:\n            try:",
        "tests/test_concurrent_turns.py::test_the_transcript_survives_concurrent_turns",
        "two turns on one session do not interleave into a transcript the "
        "provider refuses, and leave permanently malformed",
    ),
    Mutation(
        "queueing-is-silent", 87, "mini_loop/agent.py",
        '        if self._turn_lock.locked():\n            await self._send("turn_queued")',
        "        pass",
        "tests/test_concurrent_turns.py::test_a_queued_turn_is_reported",
        "a caller waiting behind a long turn is told, rather than guessing",
    ),
    Mutation(
        "external-cancel-leaves-a-dangling-tool", 88, "mini_loop/agent.py",
        "                self.close_unanswered_tools()\n"
        "                # Cancelling the await abandoned the worker thread, not the",
        "                # Cancelling the await abandoned the worker thread, not the",
        "tests/test_external_cancellation.py::test_a_dangling_tool_is_answered_as_unknown_not_failed",
        "a cancel from outside the harness -- an HTTP disconnect, a wait_for "
        "timeout -- does not leave the session refusing every later turn",
    ),
    Mutation(
        "route-bypasses-the-session", 89, "mini_loop/server.py",
        "        final = await session.run(\n"
        "            req.message,\n"
        "            run_context=_authenticated_message_context(caller),\n"
        "        )",
        "        final = await session.agent.run(\n"
        "            req.message,\n"
        "            run_context=_authenticated_message_context(caller),\n"
        "        )",
        "tests/test_entry_points.py::test_only_sessionless_callers_bypass_the_session",
        "a caller serving a user goes through the session, so its turns are "
        "counted, recorded to a trajectory and serialized",
    ),
    Mutation(
        "unreadable-skill-kills-the-loader", 90, "mini_loop/skills.py",
        "            except (OSError, UnicodeDecodeError) as exc:",
        "            except (OSError,) as exc:",
        "tests/test_skill_catalogue.py::test_a_poisoned_skills_directory_does_not_end_every_turn",
        "one undecodable byte in a skills directory does not end every turn "
        "of every session",
    ),
    Mutation(
        "skill-catalogue-unbounded", 90, "mini_loop/skills.py",
        "            if used + len(line) > MAX_SKILL_CATALOGUE:",
        "            if False:",
        "tests/test_skill_catalogue.py::test_the_catalogue_is_bounded",
        "the skill catalogue in every system prompt is bounded (100 skills "
        "measured ~100,000 tokens per request)",
    ),
    Mutation(
        "omitted-skills-are-silent", 90, "mini_loop/skills.py",
        '            lines.append(f"  [{dropped} more skill(s) omitted; catalogue is full]")',
        "            pass",
        "tests/test_skill_catalogue.py::test_omitted_skills_are_named_not_dropped_silently",
        "a skill left out of the catalogue is named, not silently absent",
    ),
    Mutation(
        "tool-payload-unbounded", 91, "mini_loop/registry.py",
        "        if _payload_size(schemas) <= MAX_TOOL_PAYLOAD:\n            return schemas",
        "        if True:\n            return schemas",
        "tests/test_tool_payload.py::test_the_payload_stays_within_budget",
        "tool definitions sent on every request are bounded (200 MCP tools "
        "measured ~212,958 tokens per request)",
    ),
    Mutation(
        "tools-dropped-before-descriptions-trimmed", 91, "mini_loop/registry.py",
        "        for limit in TOOL_DESCRIPTION_STEPS:",
        "        for limit in ():",
        "tests/test_tool_payload.py::test_descriptions_are_trimmed_before_a_tool_is_dropped",
        "a tool keeps a short description rather than disappearing: an absent "
        "tool is a capability the model cannot use",
    ),
    Mutation(
        "problem-channels-reach-nobody", 92, "mini_loop/audit.py",
        "        if name.startswith(\"_\") or name in _SPECIFICALLY_CHECKED:",
        "        if True:",
        "tests/test_problem_reporting.py::test_every_problem_channel_reaches_the_report",
        "every subsystem with somewhere to report has somewhere it is read: "
        "four of six channels reached nobody",
    ),
    Mutation(
        "system-prompt-lists-the-registry-not-the-request", 93, "mini_loop/prompts.py",
        '    tools = ", ".join(sent_names)',
        '    tools = ", ".join(agent.tools.names())',
        "tests/test_tool_payload.py::test_the_system_prompt_lists_only_sent_tools",
        "the prompt enumerates what the request carries: listing a tool whose "
        "definition was never sent tells the model a false capability",
    ),
    Mutation(
        "tool-omission-hidden-from-the-model", 93, "mini_loop/prompts.py",
        "    if not omitted:",
        "    if True:",
        "tests/test_tool_payload.py::test_the_model_is_told_what_was_omitted",
        "dropped tools are named to the model, not only to the operator: "
        "silence leaves it believing it was shown everything",
    ),
    Mutation(
        "omission-notice-rebuilds-the-payload", 93, "mini_loop/prompts.py",
        "    shown = \", \".join(omitted[:MAX_OMITTED_NAMED])",
        "    shown = \", \".join(omitted)",
        "tests/test_tool_payload.py::test_the_omission_notice_is_bounded",
        "the notice stays bounded: naming every dropped tool rebuilds the very "
        "payload the budget removed, one channel over",
    ),
    Mutation(
        "session-delete-leaks-the-background-shell", 94, "mini_loop/manager.py",
        '        background = agent.state.get("background") if agent is not None else None',
        "        background = None",
        "tests/test_session_reclamation.py::test_delete_kills_the_sessions_background_process",
        "deleting a session reclaims its background shell: unreclaimed, a "
        "process started with start_new_session survives the server itself",
    ),
    Mutation(
        "session-delete-leaves-cron-to-resurrect-it", 128, "mini_loop/manager.py",
        "        self.cron.cancel_for_session(session_id)",
        "        pass",
        "tests/test_session_reclamation.py::test_delete_stops_the_sessions_scheduled_cron_work",
        "deleting a session cancels its cron jobs: left scheduled, one fires into "
        "restore_scheduled_session and rebuilds the deleted session, undoing the "
        "delete unattended",
    ),
    Mutation(
        "session-delete-leaves-the-durable-record", 129, "mini_loop/manager.py",
        "        self.state_store.delete_session(session_id)",
        "        pass",
        "tests/test_restart_continuity.py::test_a_deleted_session_does_not_come_back_after_a_restart",
        "deleting a session removes its durable record: left behind, "
        "restore_sessions rebuilds the deleted session on the next startup, so a "
        "restart resurrects everything a user ever deleted",
    ),
    Mutation(
        "restart-builds-before-binding-the-session-owner", 188,
        "mini_loop/manager.py",
        "            session.owner = record.owner\n"
        "            session.agent = self._build_agent(",
        "            session.agent = self._build_agent(",
        "tests/test_user_resource_lifecycle.py::"
        "test_owner_is_recorded_before_first_run_and_restored_before_build",
        "startup restore binds the persisted owner before Agent construction; "
        "otherwise its skill and memory sources are frozen to anonymous",
    ),
    Mutation(
        "cron-restore-builds-before-binding-the-session-owner", 188,
        "mini_loop/manager.py",
        "        if record is not None:\n"
        "            session.owner = record.owner\n"
        "        session.agent = self._build_agent(",
        "        if record is not None:\n"
        "            pass\n"
        "        session.agent = self._build_agent(",
        "tests/test_user_resource_lifecycle.py::"
        "test_owner_is_recorded_before_first_run_and_restored_before_build",
        "cron restore binds the persisted owner before Agent construction; "
        "otherwise unattended runs use the anonymous skill and memory scope",
    ),
    Mutation(
        "shared-mcp-client-closed-with-first-session", 94, "mini_loop/manager.py",
        "            if id(client) not in still_held",
        "            if True",
        "tests/test_session_reclamation.py::test_a_shared_mcp_client_survives_a_single_delete",
        "a client held by several sessions closes with the last holder, not "
        "the first delete -- the shared-workspace rule, applied to connections",
    ),
    Mutation(
        "workspace-removal-forgets-its-turn", 94, "mini_loop/manager.py",
        # Anchored on the *async close* site (the one the named test exercises,
        # since it runs under a live loop). The bare `if remove_now:` matched
        # several sites and mutated the first -- a different branch entirely.
        "                    # live process has as its cwd is a race, not a cleanup.\n"
        "                    if remove_now:",
        "                    # live process has as its cwd is a race, not a cleanup.\n"
        "                    if False:",
        "tests/test_session_reclamation.py::test_the_workspace_outlives_the_shell",
        "when the close path owns workspace removal it must actually remove "
        "it after the shell dies, or every deleted session leaks a directory",
    ),
    Mutation(
        "unclassified-tool-falls-back-to-allowed", 95, "mini_loop/permissions.py",
        "            lambda ctx, call: _declared_risk(ctx, call) is None,",
        "            lambda ctx, call: False,",
        "tests/test_tool_risk.py::test_an_unclassified_tool_gates_upward_not_downward",
        "no risk claim gates as external, never as read: OpenWorker's own "
        "review flags its fall-back-to-READ as the standing hazard",
    ),
    Mutation(
        "external-tools-run-unchallenged", 95, "mini_loop/permissions.py",
        '            lambda ctx, call: _declared_risk(ctx, call) == "external",',
        "            lambda ctx, call: False,",
        "tests/test_tool_risk.py::test_an_external_tool_is_denied_without_an_approval_path",
        "an external-risk tool asks before acting: the deploy-substring "
        "heuristic it replaced let delete_repository run with zero events",
    ),
    Mutation(
        "mcp-risk-taken-from-the-server", 95, "mini_loop/mcp.py",
        '                 readonly=bool(annotations.get("readOnlyHint")),\n'
        '                 risk="external"),',
        '                 readonly=bool(annotations.get("readOnlyHint")),\n'
        '                 risk="read" if annotations.get("readOnlyHint") else "external"),',
        "tests/test_tool_risk.py::test_mcp_risk_is_pinned_external_whatever_the_server_claims",
        "a claim written by the untrusted side of a boundary must not lower "
        "what the boundary enforces",
    ),
    Mutation(
        "approval-timeout-falls-back-to-allow", 96, "mini_loop/approvals.py",
        "                waited=self.timeout,\n"
        "            )\n"
        "            return False",
        "                waited=self.timeout,\n"
        "            )\n"
        "            return True",
        "tests/test_approvals.py::test_nobody_answering_is_a_deny_not_a_hang",
        "an unanswered approval denies: silence is not consent",
    ),
    Mutation(
        "foreign-session-resolves-approval", 96, "mini_loop/approvals.py",
        "        if pending is None or pending.session_id != session_id:",
        "        if pending is None:",
        "tests/test_approvals.py::test_a_foreign_approval_id_behaves_like_a_missing_one",
        "an approval id from another session behaves like a missing one -- "
        "the round-80 tenancy rule, applied to approvals",
    ),
    Mutation(
        "delete-abandons-pending-approvals", 96, "mini_loop/manager.py",
        "        self.approvals.cancel_session(session_id)",
        "        pass",
        "tests/test_approvals.py::test_deleting_the_session_denies_its_pending_approvals",
        "deleting a session answers its open questions with deny instead of "
        "leaving a turn parked until the timeout",
    ),
    Mutation(
        "missing-tool-parks-for-approval", 96, "mini_loop/permissions.py",
        "    if tool is None:\n        return _MISSING",
        "    if tool is None:\n        return None",
        "tests/test_approvals.py::test_a_missing_tool_is_dispatchs_problem_not_an_approval",
        "a tool with no handler is dispatch's unknown-tool answer, not a "
        "question for a human: the collapse parked turns for the full timeout",
    ),
    Mutation(
        "cancelled-turn-abandons-the-shell", 97, "mini_loop/agent.py",
        "                self.toolset.interrupt()",
        "                pass",
        "tests/test_foreground_interrupt.py::test_cancel_kills_the_foreground_shell",
        "stopping a turn stops the shell it started: cancelling the await "
        "only abandons the thread, and the command burns until bash_timeout",
    ),
    Mutation(
        "interrupt-spares-the-commands-children", 97, "mini_loop/tools.py",
        "        for process in live:\n            _kill_group(process)",
        "        for process in live:\n            process.kill()",
        "tests/test_foreground_interrupt.py::test_cancel_kills_the_whole_group_not_just_the_shell",
        "the interrupt ends the process group: killing only the wrapping "
        "shell orphans whatever the command backgrounded",
    ),
    Mutation(
        "finished-shells-linger-in-the-live-set", 97, "mini_loop/tools.py",
        "            with self._live_lock:\n                self._live.discard(process)",
        "            with self._live_lock:\n                pass",
        "tests/test_foreground_interrupt.py::test_interrupt_is_a_no_op_when_nothing_runs",
        "completed commands leave the live set: a set that only grows makes "
        "every later interrupt walk dead processes",
    ),
    Mutation(
        "readonly-mode-asks-instead-of-refusing", 98, "mini_loop/permissions.py",
        "        mode = _session_mode(ctx)\n        if mode == \"readonly\":",
        "        mode = _session_mode(ctx)\n        if False:",
        "tests/test_permission_modes.py::test_a_readonly_session_cannot_mutate",
        "a read-only session denies mutation outright: no approval, human or "
        "hook, may mutate through it",
    ),
    Mutation(
        "auto-mode-widens-what-is-refused", 98, "mini_loop/permissions.py",
        "        for rule in self.rules:",
        "        for rule in (self.rules if _session_mode(ctx) != \"auto\" else []):",
        "tests/test_permission_modes.py::test_auto_mode_honours_custom_deny_rules",
        "full access means stop asking, not stop refusing: custom deny rules "
        "are the hook's own load-bearing case -- the built-in boundaries are "
        "double-enforced by the toolset below it and cannot show the skip",
    ),
    Mutation(
        "unknown-mode-silently-interactive", 98, "mini_loop/manager.py",
        "        if permission_mode not in PERMISSION_MODES:",
        "        if False:",
        "tests/test_permission_modes.py::test_an_unknown_mode_is_rejected_at_creation",
        "a typo'd mode is rejected loudly: 'reaonly' silently becoming "
        "interactive is a security posture the caller did not choose",
    ),
    Mutation(
        "compaction-splices-into-the-canonical-epoch", 99, "mini_loop/session.py",
        "            self._transcript_epoch += 1\n            self._persisted_messages = 0",
        "            self._persisted_messages = 0",
        "tests/test_canonical_history.py::test_the_superseded_epoch_keeps_the_original_bodies",
        "a rewrite opens a new epoch: appending the compacted transcript into "
        "the old one turns the canonical record into a chimera of both",
    ),
    Mutation(
        "ask-leaves-no-durable-row", 100, "mini_loop/approvals.py",
        # `ask` and `ask_question` register identically; the absence of
        # `kind="question"` is what pins this to the tool-approval site.
        '            tool_use_id=getattr(call, "id", "") or "",\n'
        '        )\n'
        '        self._pending[pending.approval_id] = pending\n'
        '        self._persist(pending, "pending")',
        '            tool_use_id=getattr(call, "id", "") or "",\n'
        '        )\n'
        '        self._pending[pending.approval_id] = pending',
        "tests/test_durable_approvals.py::test_every_ask_leaves_a_row_and_every_outcome_updates_it",
        "every ask leaves a durable row: without it a restart cannot tell "
        "parked-never-ran from dispatched-outcome-unknown",
    ),
    Mutation(
        "approval-preview-masked-after-serialize", 121, "mini_loop/approvals.py",
        "        secrets = getattr(ctx.agent, \"secrets\", None)\n        shown = secrets.mask_payload(call.input) if secrets is not None else call.input\n        preview = json.dumps(shown, default=str)[:INPUT_PREVIEW_CAP]",
        "        preview = json.dumps(call.input, default=str)[:INPUT_PREVIEW_CAP]\n        secrets = getattr(ctx.agent, \"secrets\", None)\n        if secrets is not None:\n            preview = secrets.mask(preview)",
        "tests/test_durable_approvals.py::test_a_json_escaping_secret_in_a_tool_argument_is_masked_in_the_preview",
        "the approval preview masks the input structure before json escapes it, "
        "so a non-ASCII credential in a tool argument does not survive into the "
        "durable approval row",
    ),
    Mutation(
        "restore-mislabels-parked-as-unknown", 100, "mini_loop/session.py",
        "        repaired = self._close_unanswered_tools(self._expire_parked_approvals())",
        "        self._expire_parked_approvals()\n"
        "        repaired = self._close_unanswered_tools()",
        "tests/test_durable_approvals.py::test_a_parked_call_restores_as_not_run",
        "a call parked on an approval never ran and may be retried; answering "
        "it as unknown gives the model exactly the wrong advice",
    ),
    Mutation(
        "everything-restores-as-not-run", 100, "mini_loop/agent.py",
        '                    "content": (overrides or {}).get(tool_use_id, UNKNOWN_RESULT),',
        '                    "content": (overrides or {}).get(tool_use_id, "[not run] safe to retry"),',
        "tests/test_durable_approvals.py::test_a_dispatched_call_still_restores_as_unknown",
        "the distinction cuts both ways: a dispatched call keeps the "
        "do-not-retry advice -- blanket not-run invites double side effects",
    ),
    Mutation(
        "recall-reads-every-owners-memory", 117, "mini_loop/memory.py",
        "        hits = await asyncio.to_thread(memory_store_for(ctx.agent).search, query)",
        '        hits = await asyncio.to_thread(ctx.state["memory"].search, query)',
        "tests/test_memory_tenant_isolation.py::test_one_owner_cannot_recall_anothers_memory",
        "recall goes through the owner-scoped store: the raw store returns "
        "every owner's memories, so one caller reads another's",
    ),
    Mutation(
        "consolidation-wipes-every-tenant", 136, "mini_loop/memory.py",
        "        return self._store.replace_all(\n"
        "            memories, owner=self._bound_owner(owner), origin=origin\n"
        "        )",
        "        return self._store.replace_all(\n"
        "            memories, owner=None, origin=origin\n"
        "        )",
        "tests/test_memory_tenant_isolation.py::test_one_owners_consolidation_does_not_wipe_anothers",
        "replace_all is owner-scoped: unscoped, one tenant's turn-end "
        "consolidation deletes every tenant's memory files, the round-117 leak "
        "class as a destructive op",
    ),
    Mutation(
        "remember-writes-as-anonymous", 117, "mini_loop/memory.py",
        "        store = memory_store_for(ctx.agent)\n        async with store.lifecycle_lock:",
        '        store = ctx.state["memory"]\n        async with store.lifecycle_lock:',
        "tests/test_memory_tenant_isolation.py::test_remember_writes_under_the_session_owner",
        "remember writes under the session owner, not the raw store's "
        "anonymous default, or every owner's memories pool together",
    ),
    Mutation(
        "scopedmemory-classification-not-enforced", 137,
        "tests/test_memory_tenant_isolation.py",
        '    harmless = {"flush"}',
        "    harmless = set()",
        "tests/test_memory_tenant_isolation.py::test_scopedmemory_scopes_every_owner_sensitive_method",
        "the completeness guard flags any public MemoryStore method not "
        "classified owner-sensitive-or-harmless, so a new one cannot ride "
        "__getattr__ unscoped the way replace_all did",
    ),
    Mutation(
        "memory-tool-builds-its-own-raw-store", 118, "mini_loop/memory.py",
        "        store = memory_store_for(ctx.agent)\n        async with store.lifecycle_lock:",
        '        store = MemoryStore(ctx.workspace / ".memory")\n        async with store.lifecycle_lock:',
        "tests/test_memory_hygiene.py::test_memory_tools_reach_the_store_only_through_the_scoped_seam",
        "a memory tool that builds its own unscoped MemoryStore is caught by "
        "the seam scan, not only by a per-tool behavioural test",
    ),
    Mutation(
        "tool-result-unmasked-before-the-journal", 116, "mini_loop/agent.py",
        "        out = self.secrets.mask(out)",
        "        out = out",
        "tests/test_no_secret_on_disk.py::test_no_recorded_sink_leaks_a_secret",
        "the tool result is masked before the action journal records it: the "
        "finish-before-mask order kept the secret in the durable actions table",
    ),
    Mutation(
        "trajectory-input-recorded-with-the-secret", 115, "mini_loop/session.py",
        "                    input_text=self._mask(message),",
        "                    input_text=message,",
        "tests/test_trajectory_masking.py::test_a_secret_in_the_input_is_masked_in_the_trajectory",
        "the trajectory masks a secret in the user input like the transcript "
        "does: the start record bypasses the event masking path",
    ),
    Mutation(
        "llm-semaphore-zero-hangs-unchecked", 114, "mini_loop/config.py",
        '        if self.max_concurrent_llm < 1:\n            raise ValueError("max_concurrent_llm must be at least 1")',
        '        if False:\n            raise ValueError("max_concurrent_llm must be at least 1")',
        "tests/test_config_validation.py::test_a_sub_one_bound_is_rejected",
        "max_concurrent_llm < 1 is rejected at construction: a Semaphore(0) "
        "hangs the agent forever on its first model call, with no error",
    ),
    Mutation(
        "bash-timeout-zero-times-out-everything", 153, "mini_loop/config.py",
        '        if self.bash_timeout < 1:\n            raise ValueError("bash_timeout must be at least 1")',
        '        if False:\n            raise ValueError("bash_timeout must be at least 1")',
        "tests/test_config_validation.py::test_a_sub_one_bound_is_rejected",
        "bash_timeout < 1 is rejected at construction: communicate(timeout=0) "
        "times out immediately, so every shell command silently fails",
    ),
    Mutation(
        "approval-timeout-zero-denies-everything", 153, "mini_loop/config.py",
        '        if self.approval_timeout <= 0:\n            raise ValueError("approval_timeout must be positive")',
        '        if False:\n            raise ValueError("approval_timeout must be positive")',
        "tests/test_config_validation.py::test_a_sub_one_bound_is_rejected",
        "approval_timeout <= 0 is rejected: wait_for(future, 0) denies every "
        "approval before anyone can answer it",
    ),
    Mutation(
        "corrupt-trajectory-dropped-in-silence", 113, "mini_loop/trajectory.py",
        "                self.problems.append(\n"
        "                    f\"{path.name}: unreadable ({type(error).__name__}); dropped \"\n"
        "                    \"from the trajectory listing\"\n"
        "                )\n"
        "                continue",
        "                continue",
        "tests/test_trajectory_corruption.py::test_the_drop_is_reported",
        "a corrupt trajectory dropped from the listing is reported, not "
        "swallowed: a silent drop looks like a recording that was never made",
    ),
    Mutation(
        "owner-map-grows-without-bound", 112, "mini_loop/manager.py",
        "            while len(owners) > MAX_REMEMBERED_OWNERS:\n                del owners[next(iter(owners))]",
        "            while False:\n                del owners[next(iter(owners))]",
        "tests/test_owner_map_bound.py::test_the_owner_map_stays_bounded_across_many_deletes",
        "the deleted-session owner map is bounded: unbounded it is memory plus "
        "O(deleted) latency on every trajectory listing that iterates it",
    ),
    Mutation(
        "answer-secret-persisted-raw", 111, "mini_loop/approvals.py",
        "        if answer is not None and self.secrets is not None:\n            answer = self.secrets.mask(answer)",
        "        if answer is not None and self.secrets is not None:\n            answer = answer",
        "tests/test_answer_masking.py::test_a_secret_in_the_answer_is_masked_in_the_row",
        "a registered secret in an ask_user answer is masked before the durable "
        "row is written -- the one sink that had it raw while all others masked",
    ),
    Mutation(
        "cron-fires-before-persisting-the-mark", 110, "mini_loop/cron.py",
        "                if job.durable:\n                    self._save()\n                self._fire(job)",
        "                self._fire(job)\n                if job.durable:\n                    self._save()",
        "tests/test_cron_crash_consistency.py::test_the_mark_is_on_disk_before_the_run_dispatches",
        "the fired mark is persisted before the run dispatches: fire-then-save "
        "leaves a crash window that re-fires the occurrence on restart",
    ),
    Mutation(
        "message-to-a-ghost-teammate-silently-lost", 109, "mini_loop/teams.py",
        "            if to not in known:",
        "            if False:",
        "tests/test_message_routing.py::test_a_message_to_a_nonexistent_teammate_is_refused",
        "a message to a name nobody consumes is refused, not confirmed: a "
        "delivery reported but never made is worse than an error",
    ),
    Mutation(
        "snip-head-splits-a-tool-use-from-its-result", 108, "mini_loop/compaction.py",
        "    if head_end < len(messages) and _message_has_tool_use(messages[head_end - 1]):",
        "    if False:",
        "tests/test_compaction_pairing.py::test_no_compaction_path_ever_orphans_a_tool_block",
        "the head keeps a tool_use's results with it: cut between them and the "
        "retained tool_use is an orphan the provider 400s on every later turn",
    ),
    Mutation(
        "snip-tail-drops-a-results-tool-use", 108, "mini_loop/compaction.py",
        "        tail_start -= 1",
        "        tail_start -= 0",
        "tests/test_compaction_pairing.py::test_no_compaction_path_ever_orphans_a_tool_block",
        "the tail pulls back to include a leading tool_result's tool_use: "
        "without it the tail opens on an orphan tool_result",
    ),
    Mutation(
        "reactive-compact-orphans-the-boundary-result", 108, "mini_loop/recovery.py",
        "            start -= 1",
        "            start -= 0",
        "tests/test_compaction_pairing.py::test_no_compaction_path_ever_orphans_a_tool_block",
        "the emergency trim keeps the boundary tool_result's tool_use: dropping "
        "it bricks the session with a permanent 400",
    ),
    Mutation(
        "observe-stream-leaks-its-subscriber", 107, "mini_loop/server.py",
        "            finally:\n                session.unsubscribe(q)\n\n        return EventSourceResponse(gen())",
        "            finally:\n                pass\n\n        return EventSourceResponse(gen())",
        "tests/test_event_stream.py::test_a_disconnect_reclaims_the_subscriber",
        "a disconnected console must have its subscriber reclaimed: an "
        "abandoned queue fills forever with events nobody drains",
    ),
    Mutation(
        "console-renders-untrusted-content-as-html", 106, "mini_loop/server.py",
        "summary.textContent=eventSummary(type,payload);",
        "summary.innerHTML=eventSummary(type,payload);",
        "tests/test_console_safety.py::test_the_console_uses_no_unsafe_dom_sink",
        "the console renders event fields through textContent, not innerHTML: "
        "a crafted tool result must not become script in the operator's browser",
    ),
    Mutation(
        "csp-lets-the-token-leave-the-origin", 106, "mini_loop/server.py",
        "    \"connect-src 'self'; \"",
        "    \"connect-src *; \"",
        "tests/test_console_safety.py::test_the_csp_blocks_token_exfiltration",
        "connect-src 'self' blocks exfiltrating the localStorage token to "
        "another origin -- the one move that matters after a content injection",
    ),
    Mutation(
        "approval-persist-fault-swallowed-silently", 105, "mini_loop/approvals.py",
        "            self.problems.append(\n"
        "                f\"approval persistence failed ({type(error).__name__}); a \"\n"
        "                \"parked approval lost here restores as UNKNOWN, not NOT_RUN\"\n"
        "            )\n"
        "            return",
        "            return",
        "tests/test_approval_persistence_faults.py::test_a_persist_fault_is_reported_not_swallowed",
        "a swallowed approval-write is reported: silence degrades round 100's "
        "restart guarantee with no signal to anyone",
    ),
    Mutation(
        "audit-parallel-check-reads-readonly-not-risk", 104, "mini_loop/audit.py",
        '            t.name for t in parallel if t.risk in ("exec", "external") or t.risk is None',
        "            t.name for t in parallel if not t.readonly and False",
        "tests/test_load_bearing_claims.py::test_a_parallel_safe_external_tool_is_a_louder_finding",
        "the audit reasons about risk, the single source of truth for mutates: "
        "reading readonly let a parallel_safe external tool pass as low-risk",
    ),
    Mutation(
        "readonly-allowed-to-drift-from-risk", 104, "mini_loop/builtins.py",
        'reg.register(Tool("ask_user", ASK_USER["description"], ASK_USER["input_schema"], _ask_user, readonly=True, risk="read"))',
        'reg.register(Tool("ask_user", ASK_USER["description"], ASK_USER["input_schema"], _ask_user, risk="read"))',
        "tests/test_tool_risk.py::test_shipped_readonly_agrees_with_risk",
        "readonly and risk both encode 'mutates' and must agree on built-ins: "
        "two sources of truth drift, and these two already had",
    ),
    Mutation(
        "running-marker-set-before-the-lock", 103, "mini_loop/session.py",
        "        async with self.lock:\n"
        "            # A caller may have passed the first check, then queued behind a\n"
        "            # turn while SessionManager.stop/delete closed admission.\n"
        "            if not self._accepting_runs:\n"
        "                raise RuntimeError(\n"
        "                    self._closed_reason or \"session is not accepting turns\"\n"
        "                )\n"
        "            # `_running` is the task that HOLDS the lock -- the turn actually\n"
        "            # in flight -- not whoever most recently entered run(). Set before\n"
        "            # the lock, a caller still queued on it (a cron fire into a busy\n"
        "            # session) overwrote the running task's reference, and cancel()\n"
        "            # then stopped the queued turn while the running one continued.\n"
        "            self._running = asyncio.current_task()",
        "        self._running = asyncio.current_task()\n"
        "        async with self.lock:\n"
        "            # A caller may have passed the first check, then queued behind a\n"
        "            # turn while SessionManager.stop/delete closed admission.\n"
        "            if not self._accepting_runs:\n"
        "                raise RuntimeError(\n"
        "                    self._closed_reason or \"session is not accepting turns\"\n"
        "                )\n"
        "            # `_running` is the task that HOLDS the lock -- the turn actually\n"
        "            # in flight -- not whoever most recently entered run(). Set before\n"
        "            # the lock, a caller still queued on it (a cron fire into a busy\n"
        "            # session) overwrote the running task's reference, and cancel()\n"
        "            # then stopped the queued turn while the running one continued.",
        "tests/test_cancel_targets_running_turn.py::test_cancel_hits_the_running_turn_not_the_queued_one",
        "the cancel target is the lock holder, set inside the lock: assigned "
        "before it, a queued cron fire steals the marker and cancel misfires",
    ),
    Mutation(
        "question-answer-collapsed-to-a-bool", 102, "mini_loop/approvals.py",
        '        if pending.kind == "question":\n'
        "            value = str(answer) if (allowed and answer is not None) else None\n"
        "            pending.future.set_result(value)",
        '        if False:\n'
        "            value = str(answer) if (allowed and answer is not None) else None\n"
        "            pending.future.set_result(value)",
        "tests/test_ask_user.py::test_the_answer_reaches_the_model",
        "a question's answer is text, not a boolean: collapsing it hands the "
        "model True where it needed the words",
    ),
    Mutation(
        "ask-user-hangs-without-a-broker", 102, "mini_loop/builtins.py",
        '    broker = getattr(ctx.state.get("manager"), "approvals", None)\n'
        "    if broker is None:",
        '    broker = getattr(ctx.state.get("manager"), "approvals", None)\n'
        "    if False:",
        "tests/test_features.py::test_ask_user_without_a_broker_says_so",
        "ask_user on a broker-less surface reports its absence rather than "
        "crashing on a None call -- a bare Agent has nobody to ask",
    ),
    Mutation(
        "steering-is-never-delivered", 101, "mini_loop/manager.py",
        "        if steering_injector not in self.injectors:\n"
        "            self.injectors.append(steering_injector)",
        "        pass",
        "tests/test_steering.py::test_a_mid_turn_steer_reaches_the_model_mid_turn",
        "a queued steer must reach the model: a queue nothing drains is the "
        "409 with extra steps",
    ),
    Mutation(
        "steer-drops-the-callers-words", 101, "mini_loop/session.py",
        "        self._steering.append(text)",
        "        pass",
        "tests/test_steering.py::test_an_idle_steer_opens_the_next_turn",
        "steer queues the text, not just a happy status code: dropping it "
        "silently is worse than the 409 it replaced",
    ),
    Mutation(
        "steer-queue-unbounded", 132, "mini_loop/session.py",
        "        if len(self._steering) > MAX_STEER_QUEUE:\n            del self._steering[0]",
        "        pass",
        "tests/test_steering.py::test_steering_is_bounded_in_size_and_count",
        "the steering queue is capped: joined into one injected message, an "
        "unbounded number of steers floods the context (round 50's MAX_INBOX, "
        "here for steering)",
    ),
    Mutation(
        "steer-size-unbounded", 132, "mini_loop/session.py",
        '        if len(text) > MAX_STEER_CHARS:\n            text = text[:MAX_STEER_CHARS] + "\\n[steer truncated]"',
        "        pass",
        "tests/test_steering.py::test_an_oversized_steer_is_truncated_with_a_marker",
        "an oversized steer is truncated: one interjection is injected whole into "
        "the transcript, so an unbounded steer floods the context (round 50's "
        "MAX_CONTENT, here for steering)",
    ),
    Mutation(
        "a-stranger-steers-the-session", 101, "mini_loop/server.py",
        # Re-anchored in round 194: the route gained an idle branch, so the
        # ownership check now sits before `if not session.busy:`.
        '        session = _require(request, session_id)\n'
        '        if not session.busy:',
        '        session = _manager(request).get(session_id)\n'
        '        if not session.busy:',
        "tests/test_steering.py::test_steering_over_http_is_owner_scoped",
        "steering injects text into someone's running turn: unscoped, it is "
        "prompt injection as a service",
    ),
    Mutation(
        "foreign-caller-reads-the-transcript", 99, "mini_loop/server.py",
        '    async def read_transcript(request: Request, session_id: str,\n'
        '                              epoch: int | None = None):\n'
        '        """One epoch of the durable transcript -- the current one by default.\n'
        '\n'
        '        Superseded epochs are the canonical record of what the agent actually\n'
        '        saw before a compaction rewrote its history (storage.py); this is the\n'
        '        operator\'s way to read them. Persisted rows are already masked.\n'
        '        """\n'
        '        session = _require(request, session_id)',
        '    async def read_transcript(request: Request, session_id: str,\n'
        '                              epoch: int | None = None):\n'
        '        session = _manager(request).get(session_id)',
        "tests/test_canonical_history.py::test_the_canonical_record_is_readable_over_http",
        "the transcript endpoint is owner-scoped like every other session "
        "surface: a stranger reads 404, not someone else's conversation",
    ),
    Mutation(
        "spill-save-failure-breaks-the-tool-call", 171, "mini_loop/tools.py",
        '        except Exception:\n'
        '            return ""\n'
        '        return (\n'
        '            f"\\n[full output preserved: {ref.locator} ({ref.bytes:,} bytes); "\n'
        '            f"{ref.retrieval_hint}]"\n'
        '        )',
        '        except Exception as exc:\n'
        '            return f"\\nError: spill failed: {exc}"\n'
        '        return (\n'
        '            f"\\n[full output preserved: {ref.locator} ({ref.bytes:,} bytes); "\n'
        '            f"{ref.retrieval_hint}]"\n'
        '        )',
        "tests/test_spill.py::test_a_failing_store_keeps_the_preview",
        "preservation is best-effort: a broken spill store keeps the truncated "
        "preview and never turns a successful tool call into an error",
    ),
    Mutation(
        "spill-artifact-is-world-readable", 171, "mini_loop/spill.py",
        '        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)',
        '        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o644)',
        "tests/test_spill.py::test_storage_is_private",
        "a spill artifact holds command output that can carry sensitive data; "
        "it is created owner-only, never world-readable",
    ),
    Mutation(
        "spill-follows-a-planted-symlink", 171, "mini_loop/spill.py",
        '        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)\n'
        '        try:\n'
        '            os.write(fd, data)',
        '        fd = os.open(path, os.O_WRONLY | os.O_CREAT, 0o600)\n'
        '        try:\n'
        '            os.write(fd, data)',
        "tests/test_spill.py::test_a_planted_symlink_cannot_redirect_the_write",
        "exclusive create refuses anything already at the path, including a "
        "planted symlink, instead of following it",
    ),
    Mutation(
        "guard-abstention-short-circuits", 172, "mini_loop/registry.py",
        '        for h in self._hooks:\n'
        '            denial = await h.guard_tool(ctx, call)\n'
        '            if denial is not None:\n'
        '                return str(denial)\n'
        '        return None',
        '        for h in self._hooks:\n'
        '            return await h.guard_tool(ctx, call)\n'
        '        return None',
        "tests/test_tool_pipeline.py::test_every_guard_runs_an_abstention_cannot_allow",
        "every guard runs: an abstention delegates to the next guard instead "
        "of short-circuiting a stricter one, so no ordering widens policy",
    ),
    Mutation(
        "post-rewrites-a-denial", 172, "mini_loop/registry.py",
        '        if denied:\n'
        '            # A deny is final. Post hooks used to receive denials through the\n'
        '            # same replacement path as successes, so a later hook returning a\n'
        '            # string rewrote the denial into whatever it pleased -- policy\n'
        '            # ordering was policy. Observers see the denial via `result`.\n'
        '            return output',
        '        if False:\n'
        '            return output',
        "tests/test_tool_pipeline.py::test_post_cannot_replace_a_denied_result",
        "a deny is final: the post layer structurally cannot replace a denied "
        "result with a fabricated success",
    ),
    Mutation(
        "result-observer-breaks-the-call", 172, "mini_loop/registry.py",
        '            try:\n'
        '                await h.on_result(ctx, call, output, denied=denied, failed=failed)\n'
        '            except Exception as error:\n'
        '                self.problems.append(',
        '            try:\n'
        '                await h.on_result(ctx, call, output, denied=denied, failed=failed)\n'
        '            except Exception:\n'
        '                raise\n'
        '            if False:\n'
        '                self.problems.append(',
        "tests/test_tool_pipeline.py::test_a_throwing_observer_is_contained_and_the_next_still_runs",
        "the result dispatcher contains observer exceptions: one bad "
        "subscriber never breaks the tool call it observes",
    ),
    Mutation(
        "agent-skips-the-guard-layer", 172, "mini_loop/agent.py",
        '                decision = await self.hooks.guard_tool(ctx, call)',
        '                decision = None',
        "tests/test_tool_pipeline.py::test_the_agent_runs_guards_and_a_guard_denial_reaches_the_model",
        "the agent consults the monotonic guard layer on the final rewritten "
        "arguments before every tool body",
    ),
    Mutation(
        "injected-input-rides-unlogged", 173, "mini_loop/session.py",
        '        try:\n'
        '            self._flush_messages()\n'
        '        except LeaseLost:\n'
        '            raise',
        '        try:\n'
        '            pass\n'
        '        except LeaseLost:\n'
        '            raise',
        "tests/test_transcript_invariant.py::test_an_injected_message_is_durable_before_the_model_sees_it",
        "the transcript guard flushes injected input to the durable log "
        "before the model request that carries it",
    ),
    Mutation(
        "transcript-coverage-not-asserted", 173, "mini_loop/session.py",
        '        count = self.state_store.message_count(self.id)\n'
        '        if count != len(messages):',
        '        count = self.state_store.message_count(self.id)\n'
        '        if False:',
        "tests/test_transcript_invariant.py::test_a_model_visible_input_that_bypasses_the_log_fails_loud",
        "model-visible means logged: a request the durable epoch does not "
        "cover raises an attributed invariant error instead of proceeding",
    ),
    Mutation(
        "agent-skips-the-transcript-guard", 173, "mini_loop/agent.py",
        '            if self.transcript_guard is not None:\n'
        '                self.transcript_guard(self.messages)',
        '            if False:\n'
        '                self.transcript_guard(self.messages)',
        "tests/test_transcript_invariant.py::test_a_model_visible_input_that_bypasses_the_log_fails_loud",
        "the agent consults the transcript guard before every model request; "
        "skipping it silently retires the model-visible-means-logged rule",
    ),
    Mutation(
        "restored-cron-fires-unattended", 174, "mini_loop/cron.py",
        '                if job.id not in self._armed:',
        '                if False:',
        "tests/test_cron_activation.py::test_a_restored_job_is_disarmed_and_does_not_fire",
        "a durable cron job restored from disk is a schedule, not an "
        "authorization: it does not fire until an operator re-arms it",
    ),
    Mutation(
        "cron-activation-rides-the-load-path", 174, "mini_loop/cron.py",
        '                self.jobs[job.id] = job\n'
        '\n'
        '\n'
        '_SCHEDULE = {',
        '                self.jobs[job.id] = job\n'
        '                self._armed.add(job.id)\n'
        '\n'
        '\n'
        '_SCHEDULE = {',
        "tests/test_cron_activation.py::test_a_restored_job_is_disarmed_and_does_not_fire",
        "activation never rides the durable load path: a restart must not "
        "trust yesterday's authorization edge",
    ),
    Mutation(
        "stranger-arms-someone-elses-cron", 174, "mini_loop/cron.py",
        '        if session_id is not None and job.session_id != session_id:\n'
        '            return f"Error: no such job {job_id}"\n'
        '        self._armed.add(job_id)',
        '        self._armed.add(job_id)',
        "tests/test_cron_activation.py::test_arm_is_session_scoped_like_cancel",
        "arm is scoped like cancel: a caller can only re-authorize jobs that "
        "belong to its own session",
    ),
    Mutation(
        "retry-without-a-smaller-surface", 175, "mini_loop/recovery.py",
        '                    cost_before = estimate_tokens(kwargs["messages"])\n'
        '                    compacted = reactive_compact(kwargs["messages"])\n'
        '                    if estimate_tokens(compacted) >= cost_before:',
        '                    cost_before = estimate_tokens(kwargs["messages"])\n'
        '                    compacted = reactive_compact(kwargs["messages"])\n'
        '                    if False:',
        "tests/test_retry_requires_progress.py::test_an_unshrinkable_surface_fails_without_a_retry",
        "a context-overflow retry is issued only when compaction actually "
        "shrank the surface; an identical retry is a guaranteed second "
        "overflow billed as recovery",
    ),
    Mutation(
        "stale-envelope-anchor-trusted", 176, "mini_loop/metering.py",
        '        if (\n'
        '            envelope is not None\n'
        '            and self._anchor_envelope is not None\n'
        '            and envelope != self._anchor_envelope\n'
        '        ):\n'
        '            return estimate_tokens(messages)\n'
        '        return self.used(messages)',
        '        return self.used(messages)',
        "tests/test_metering.py::test_an_envelope_change_sets_the_anchor_aside",
        "an anchor read under one request envelope is set aside when the "
        "envelope changes; trusting it under-counts, the direction that ends "
        "in a hard overflow",
    ),
    Mutation(
        "calibration-poisoned-across-envelopes", 176, "mini_loop/metering.py",
        '        same_envelope = (\n'
        '            envelope is None\n'
        '            or self._anchor_envelope is None\n'
        '            or envelope == self._anchor_envelope\n'
        '        )',
        '        same_envelope = True',
        "tests/test_metering.py::test_calibration_is_not_learned_across_an_envelope_change",
        "the calibration ratio is learned only from readings under one "
        "envelope; a cross-envelope delta contains schema bytes, not growth",
    ),
    Mutation(
        "summary-failure-kills-the-turn", 177, "mini_loop/compaction.py",
        '            try:\n'
        '                await self.compact(agent)\n'
        '            except asyncio.CancelledError:\n'
        '                raise\n'
        '            except Exception as error:',
        '            try:\n'
        '                await self.compact(agent)\n'
        '            except asyncio.CancelledError:\n'
        '                raise\n'
        '            except Exception as error:\n'
        '                raise\n'
        '            if False:',
        "tests/test_compaction_failure.py::test_a_failed_summary_keeps_the_transcript_and_reports",
        "a failed summary closes the attempt with the surface unchanged and "
        "the turn alive; the next request's own error stays authoritative",
    ),
    Mutation(
        "empty-summary-replaces-the-transcript", 177, "mini_loop/compaction.py",
        '        if not summary.strip():',
        '        if False:',
        "tests/test_compaction_failure.py::test_an_empty_summary_is_a_failure_not_a_replacement",
        "an empty summary is a failed attempt, never a license to replace "
        "the whole transcript with a file path",
    ),
    Mutation(
        "broken-classifier-goes-parallel", 178, "mini_loop/registry.py",
        '        if self.mode_for is not None:\n'
        '            try:\n'
        '                mode = self.mode_for(call)\n'
        '            except Exception:\n'
        '                return "exclusive"\n'
        '            return mode if mode in ("parallel", "exclusive") else "exclusive"',
        '        if self.mode_for is not None:\n'
        '            try:\n'
        '                mode = self.mode_for(call)\n'
        '            except Exception:\n'
        '                return "parallel"\n'
        '            return mode if mode in ("parallel", "exclusive") else "parallel"',
        "tests/test_execution_mode.py::test_a_broken_classifier_degrades_to_a_barrier",
        "a classifier that fails degrades to exclusive: a barrier cannot lose "
        "an update, a wrongly-parallel write can",
    ),
    Mutation(
        "foreground-bash-classified-parallel", 178, "mini_loop/builtins.py",
        '            bash_tool.mode_for = (\n'
        '                lambda call: "parallel"\n'
        '                if call.input.get("run_in_background")\n'
        '                else "exclusive"\n'
        '            )',
        '            bash_tool.mode_for = lambda call: "parallel"',
        "tests/test_execution_mode.py::test_background_bash_is_parallel_only_when_backgrounding_exists",
        "only a bash call that will actually background is parallel; a "
        "foreground bash owns the workspace and must barrier the batch",
    ),
    Mutation(
        "rejected-plan-still-exits", 179, "mini_loop/plan_mode.py",
        '            approved, feedback = await approval(ctx, text)\n'
        '            if not approved:',
        '            approved, feedback = await approval(ctx, text)\n'
        '            if False:',
        "tests/test_plan_mode.py::test_keep_planning_is_a_failed_call_with_feedback",
        "keep-planning is a failed call carrying reviewer feedback; a "
        "rejected plan must not silently leave plan mode",
    ),
    Mutation(
        "exit-outside-plan-mode-flips-state", 179, "mini_loop/plan_mode.py",
        '        if not plan_mode_active(agent):',
        '        if False:',
        "tests/test_plan_mode.py::test_exit_outside_plan_mode_fails_without_changing_anything",
        "exit_plan_mode stays registered for catalog stability; calling it "
        "outside plan mode is a readable error, not a state flip",
    ),
    Mutation(
        "restore-forgets-plan-mode", 179, "mini_loop/session.py",
        '        agent.state["plan_mode"] = fold_plan_mode(logged_events)',
        '        agent.state["plan_mode"] = False',
        "tests/test_plan_mode.py::test_restore_folds_plan_mode_from_the_log",
        "plan mode is log-only whole-value state: restore folds the last "
        "logged flip instead of silently resetting it",
    ),
    Mutation(
        "goal-mutation-ignores-the-revision", 180, "mini_loop/goals.py",
        '    if int(revision) != goal["revision"]:',
        '    if False:',
        "tests/test_goals.py::test_mutations_are_compare_and_set",
        "every goal mutation is compare-and-set: a stale writer is refused "
        "and told the current revision, so two consumers cannot fight blind",
    ),
    Mutation(
        "goal-continues-past-the-round-cap", 180, "mini_loop/goals.py",
        '        if goal["rounds_started"] >= goal["max_rounds"]:\n'
        '            goal["revision"] += 1\n'
        '            goal["phase"] = "blocked"',
        '        if False:\n'
        '            goal["revision"] += 1\n'
        '            goal["phase"] = "blocked"',
        "tests/test_goals.py::test_continuation_consumes_rounds_and_blocks_at_the_cap",
        "the round budget is a hard cap: exhausting it blocks the goal with "
        "a stable code instead of continuing forever",
    ),
    Mutation(
        "untrusted-turn-arms-a-goal", 180, "mini_loop/goals.py",
        '        if ctx.run_context is None or ctx.run_context.authority != EXPLICIT_HUMAN:',
        '        if False:',
        "tests/test_goals.py::test_arming_mutations_require_explicit_human_authority",
        "create and resume arm unattended continuation; that edge belongs to "
        "an authenticated human, never a cron-fired or delegated turn",
    ),
    Mutation(
        "restored-goal-comes-back-armed", 180, "mini_loop/session.py",
        '        agent.state["goal_armed"] = False',
        '        agent.state["goal_armed"] = True',
        "tests/test_goals.py::test_restore_folds_the_goal_but_comes_back_disarmed",
        "a restored goal is a fact, not an authorization: activation never "
        "survives restore",
    ),
    Mutation(
        "timeout-hides-the-diagnostic-output", 181, "mini_loop/tools.py",
        '        if self.error is not None:\n'
        '            return f"{rendered}\\n{self.error}" if rendered else self.error\n'
        '        return rendered or "(no output)"',
        '        if self.error is not None:\n'
        '            return self.error\n'
        '        return rendered or "(no output)"',
        "tests/test_command_result.py::test_timeout_retains_metadata_and_masked_partial_streams",
        "orthogonal outcomes report independently: a timeout carries the "
        "partial output that explains it, never the error alone",
    ),
    Mutation(
        "workspace-removal-follows-a-link", 181, "mini_loop/manager.py",
        '        if path.is_symlink():\n'
        '            path.unlink()\n'
        '            return\n'
        '        shutil.rmtree(path, ignore_errors=True)',
        '        shutil.rmtree(path.resolve(), ignore_errors=True)',
        "tests/test_defensive_patterns.py::test_a_link_shaped_workspace_is_unlinked_never_followed",
        "a link-shaped workspace is unlinked, never resolved and deleted "
        "through: recursive removal is reserved for known real directories",
    ),
    Mutation(
        "event-sink-failure-kills-the-turn", 181, "mini_loop/session.py",
        '            try:\n'
        '                res = self._event_sink(event)\n'
        '                if inspect.isawaitable(res):\n'
        '                    await res\n'
        '            except Exception as error:\n'
        '                self.sink_error = f"{type(error).__name__}: {error}"',
        '            res = self._event_sink(event)\n'
        '            if inspect.isawaitable(res):\n'
        '                await res',
        "tests/test_defensive_patterns.py::test_a_throwing_event_sink_cannot_kill_the_turn",
        "the event sink is a contained observer: one that throws is reported "
        "through info(), never allowed to kill the turn it observes",
    ),
    Mutation(
        "posture-boilerplate-accepted", 182, "tools/verify_invariants.py",
        '            twin = explanations.get(explanation)\n'
        '            if twin:',
        '            twin = explanations.get(explanation)\n'
        '            if False:',
        "tests/test_invariant_posture.py::test_duplicated_boilerplate_is_rejected",
        "two modules sharing one explanation is the signature of pasted "
        "boilerplate, which the posture verifier must refuse",
    ),
    Mutation(
        "posture-declaration-points-at-nothing", 182, "tools/verify_invariants.py",
        '            symbol = match.group(1).split(".")[-1]\n'
        '            if symbol not in _module_names(tree):',
        '            symbol = match.group(1).split(".")[-1]\n'
        '            if False:',
        "tests/test_invariant_posture.py::test_a_declaration_pointing_at_nothing_is_rejected",
        "a RUNTIME_INVARIANT must name a symbol that exists in its module; a "
        "declaration pointing at nothing is the lie the verifier exists to catch",
    ),
    Mutation(
        "task-tool-bypasses-the-provider-seam", 183, "mini_loop/agent.py",
        '        summary = await self.subagents.run(\n'
        '            self,\n'
        '            prompt=prompt,\n'
        '            agent_type=agent_type,\n'
        '            run_context=parent_context,\n'
        '        )',
        '        from .subagents import InProcessSubagents\n'
        '        summary = await InProcessSubagents().run(\n'
        '            self,\n'
        '            prompt=prompt,\n'
        '            agent_type=agent_type,\n'
        '            run_context=parent_context,\n'
        '        )',
        "tests/test_subagent_provider.py::test_a_custom_provider_substitutes_and_telemetry_still_fires",
        "the task tool executes through the injected subagent provider, not "
        "a hard-wired in-process construction",
    ),
    Mutation(
        "subagent-lineage-dropped", 183, "mini_loop/subagents.py",
        '        lineage = {\n'
        '            "parent": parent.label,\n'
        '            "delegation_depth": parent.depth + 1,\n'
        '        }',
        '        lineage = {}',
        "tests/test_subagent_provider.py::test_the_default_provider_records_lineage_as_data",
        "lineage is carried as data on the child (who delegated, at what "
        "depth) so accountability survives without scope inheritance",
    ),
    Mutation(
        "diagnostics-escapes-the-workspace", 184, "mini_loop/diagnostics.py",
        '            try:\n'
        '                # The same confinement every file tool has: a diagnostics\n'
        '                # request must not become a read primitive outside the\n'
        '                # workspace.\n'
        '                target = ctx.agent.toolset.safe_path(path)\n'
        '            except ValueError as error:\n'
        '                return f"Error: {error}"',
        '            from pathlib import Path\n'
        '            target = Path(ctx.agent.toolset.workspace / path).resolve()',
        "tests/test_diagnostics_and_query.py::test_the_tool_names_its_scope_and_confines_paths",
        "a diagnostics request is workspace-confined like every file tool; "
        "it must not become a read primitive outside the workspace",
    ),
    Mutation(
        "transcript-search-unbounded", 184, "mini_loop/session_query.py",
        # Re-anchored in round 188: the search now returns a coverage-carrying
        # dict, so the early exit hands back `result` rather than `matches`.
        '            if len(matches) >= MAX_MATCHES:\n'
        '                return result',
        '            if False:\n'
        '                return result',
        "tests/test_diagnostics_and_query.py::test_search_is_bounded",
        "a transcript search returns a bounded match list; an unbounded one "
        "re-floods the context the epochs exist to relieve",
    ),
    Mutation(
        "trace-viewer-renders-raw-content", 185, "mini_loop/trace_view.py",
        '    return html.escape(str(value), quote=True)',
        '    return str(value)',
        "tests/test_trace_view.py::test_tool_output_script_is_escaped",
        "every string on the trace page passes the escaping chokepoint; a "
        "tool result containing <script> renders as text, never as markup",
    ),
    Mutation(
        "trace-viewer-fabricates-a-duration", 185, "mini_loop/trace_view.py",
        '    if duration_ms is None:\n'
        '        return "in flight"',
        '    if duration_ms is None:\n'
        '        return "0 ms"',
        "tests/test_trace_view.py::test_an_unfinished_span_reports_in_flight_not_a_duration",
        "a span with no end event says `in flight`; the viewer never invents "
        "a duration for work whose end was not recorded",
    ),
    Mutation(
        "trace-viewer-cap-goes-silent", 185, "mini_loop/trace_view.py",
        '    omitted = 0\n'
        '    if len(rows) > MAX_ROWS:',
        '    omitted = 0\n'
        '    if False:',
        "tests/test_trace_view.py::test_long_ledger_keeps_tail_and_names_the_omission",
        "a ledger longer than MAX_ROWS keeps the tail and states the "
        "omission; a cap nobody mentions reads as full coverage",
    ),
    Mutation(
        "trace-view-route-skips-ownership", 185, "mini_loop/server.py",
        '            await _owned_trajectory_summary(request, store, trajectory_id)\n'
        '            size = await asyncio.to_thread(store.byte_size, trajectory_id)\n'
        '            if size > MAX_TRAJECTORY_JSON_BYTES:\n'
        '                raise HTTPException(\n'
        '                    status_code=413,\n'
        '                    detail=(\n'
        '                        f"trajectory is {size:,} bytes; too large to render as "\n'
        '                        f"one page (limit {MAX_TRAJECTORY_JSON_BYTES:,}). "',
        '            size = await asyncio.to_thread(store.byte_size, trajectory_id)\n'
        '            if size > MAX_TRAJECTORY_JSON_BYTES:\n'
        '                raise HTTPException(\n'
        '                    status_code=413,\n'
        '                    detail=(\n'
        '                        f"trajectory is {size:,} bytes; too large to render as "\n'
        '                        f"one page (limit {MAX_TRAJECTORY_JSON_BYTES:,}). "',
        "tests/test_trace_view.py::test_view_route_is_scoped_like_its_json_siblings",
        "the HTML view is scoped exactly like the JSON routes beside it: "
        "someone else's trajectory is 404 before any bulk read",
    ),
    Mutation(
        "config-guesses-a-number", 186, "mini_loop/config.py",
        '    try:\n'
        '        return int(value)\n'
        '    except ValueError:\n'
        '        _reject(name, value, "an integer")',
        '    try:\n'
        '        return int(value)\n'
        '    except ValueError:\n'
        '        return default',
        "tests/test_config_validation.py::test_a_unit_suffixed_integer_refuses_to_boot",
        "an unparseable numeric setting refuses to boot; falling back to the "
        "default runs with a configuration nobody wrote",
    ),
    Mutation(
        "config-guesses-a-duration", 186, "mini_loop/config.py",
        '    try:\n'
        '        return float(value)\n'
        '    except ValueError:\n'
        '        _reject(name, value, "a number")',
        '    try:\n'
        '        return float(value)\n'
        '    except ValueError:\n'
        '        return default',
        "tests/test_config_validation.py::test_a_garbled_float_refuses_to_boot",
        "an unparseable float setting refuses to boot instead of silently "
        "meaning the default",
    ),
    Mutation(
        "config-reads-a-typo-as-true", 186, "mini_loop/config.py",
        '    if normalized in _FALSE:\n'
        '        return False\n'
        '    _reject(name, value, f"a boolean ({\'/\'.join(_TRUE)} or 0/false/no/off)")',
        '    if normalized in _FALSE:\n'
        '        return False\n'
        '    return True',
        "tests/test_config_validation.py::test_a_typoed_boolean_refuses_to_boot",
        "an unknown boolean spelling refuses to boot; reading it as True made "
        "a typo silently keep full-content recording on for an operator who "
        "had turned it off",
    ),
    Mutation(
        "early-stop-swallowed-by-commentary", 187, "mini_loop/agent.py",
        '        self.last_text = (\n'
        '            f"{headline}\\nPartial output before the stop:\\n{self.last_text}"\n'
        '            if self.last_text else headline\n'
        '        )',
        '        self.last_text = self.last_text or headline',
        "tests/test_round_exhaustion.py::test_an_exhausted_run_reports_the_stop_before_the_partial_text",
        "an early-stopped run leads with the stop headline and appends the "
        "partial text; `last_text or marker` showed the marker only to runs "
        "that had nothing to mislead with",
    ),
    Mutation(
        "stuck-halt-bypasses-the-stop-rule", 187, "mini_loop/agent.py",
        '        if halted:\n'
        '            self._mark_stopped(f"[stopped: {signal.detail}]")\n'
        '            return False',
        '        if halted:\n'
        '            self.last_text = self.last_text or f"[stopped: {signal.detail}]"\n'
        '            return False',
        "tests/test_round_exhaustion.py::test_an_exhausted_subagent_is_not_a_clean_summary",
        "the stuck halt reports through the same one-owner stop rule as round "
        "exhaustion; a second private fallback re-creates the defect the rule "
        "exists to remove",
    ),
    Mutation(
        "session-query-scan-cap-goes-silent", 188, "mini_loop/session_query.py",
        '        coverage = ""\n'
        '        if result["epochs_skipped"]:',
        '        coverage = ""\n'
        '        if False:',
        "tests/test_diagnostics_and_query.py::test_the_tool_renders_the_coverage_caveat",
        "a transcript search that skipped epochs says so in the answer "
        "itself; a bare 'No matches' over a partial scan reads as 'nothing "
        "anywhere in history'",
    ),
    Mutation(
        "trace-viewer-flattens-subagents", 189, "mini_loop/trace_view.py",
        '        depth = int(event.get("depth") or 0)\n'
        '        nest = {"depth": depth, "agent": event.get("agent")} if depth else {}',
        '        depth = int(event.get("depth") or 0)\n'
        '        nest = {}',
        "tests/test_trace_view.py::test_subagent_rows_are_nested_not_flattened",
        "child-agent rows keep their delegation depth and label; flattened, "
        "a subagent's bash is indistinguishable from the parent's",
    ),
    Mutation(
        "trace-viewer-steps-count-subagent-rounds", 189, "mini_loop/trace_view.py",
        '            if event.get("purpose") == "agent_turn" and depth == 0:',
        '            if event.get("purpose") == "agent_turn":',
        "tests/test_trace_view.py::test_step_markers_ignore_subagent_model_calls",
        "steps are the parent loop's structure; a child's model calls must "
        "not advance the turn's step count",
    ),
    Mutation(
        "transcript-guard-checks-count-not-content", 190, "mini_loop/session.py",
        '        for index, recorded in enumerate(self._persisted_digests):\n'
        '            if _row_digest(messages[index]) != recorded:',
        '        for index, recorded in enumerate(()):\n'
        '            if _row_digest(messages[index]) != recorded:',
        "tests/test_transcript_invariant.py::test_a_flushed_row_mutated_in_place_fails_loud",
        "the transcript invariant verifies content, not only count: a flushed "
        "row mutated in place would let the model see text the durable "
        "record never held",
    ),
    Mutation(
        "restore-forgets-the-digests", 190, "mini_loop/session.py",
        '        self._persisted_digests = [_row_digest(m) for m in agent.messages]',
        '        self._persisted_digests = []',
        "tests/test_transcript_invariant.py::test_restore_seeds_the_digests",
        "the content check survives a restart; unseeded digests would watch "
        "only rows flushed in the current process life",
    ),
    Mutation(
        "user-resource-root-world-readable", 191, "mini_loop/user_resources.py",
        '        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)\n'
        '        self.root.chmod(0o700)',
        '        self.root.mkdir(parents=True, exist_ok=True)',
        "tests/test_user_resource_lifecycle.py::test_a_preexisting_lax_root_is_tightened",
        "the resolver root is private and re-tightened on reuse; a "
        "default-mode tree reads as world-readable memories",
    ),
    Mutation(
        "owner-directories-world-readable", 191, "mini_loop/user_resources.py",
        '        path.mkdir(parents=True, exist_ok=True, mode=0o700)\n'
        '        path.chmod(0o700)',
        '        path.mkdir(parents=True, exist_ok=True)',
        "tests/test_user_resource_lifecycle.py::test_resource_directories_are_private",
        "every per-owner directory (owner root, skills, memory) is 0700 like "
        "the spill and trajectory trees that set the standard",
    ),
    Mutation(
        "steer-promises-queued-without-durability", 192, "mini_loop/session.py",
        '        try:\n'
        '            self._persist_session_record()\n'
        '        except Exception as error:\n'
        '            self.persist_error = f"{type(error).__name__}: {error}"\n'
        '        return len(self._steering)',
        '        return len(self._steering)',
        "tests/test_steering.py::test_a_queued_steer_survives_a_restart",
        "steer() answers 'queued' only after the queue is durable; an idle "
        "session's steer must survive the process that accepted it",
    ),
    Mutation(
        "restore-forgets-queued-steers", 192, "mini_loop/manager.py",
        '        if record.pending_steering:',
        '        if False:',
        "tests/test_steering.py::test_a_queued_steer_survives_a_restart",
        "restore reseeds the undelivered steering queue from the session "
        "record; the promise survives the process",
    ),
    Mutation(
        "steer-record-holds-raw-text", 192, "mini_loop/session.py",
        '                pending_steering=tuple(\n'
        '                    self._mask(queued) for queued in self._steering\n'
        '                ),',
        '                pending_steering=tuple(self._steering),',
        "tests/test_steering.py::test_the_durable_queue_holds_the_masked_form",
        "the durable steering queue is masked like every other durable "
        "projection; a pasted secret must not reach the sessions table raw",
    ),
    Mutation(
        "idle-steer-parks-forever", 194, "mini_loop/server.py",
        '        if not session.busy:',
        '        if False:',
        "tests/test_steering.py::test_an_idle_http_steer_starts_a_turn",
        "an idle agent hears an HTTP steer by running it as a turn (dsh: "
        "steer carries wakeup; OpenWorker: an idle session starts a fresh "
        "turn); parking it durably could wait forever",
    ),
    Mutation(
        "steering-event-drops-the-words", 196, "mini_loop/session.py",
        '    await agent._send(\n'
        '        "steering_delivered",\n'
        '        count=len(drained),\n'
        '        text=body[:DISPLAY_CAP],\n'
        '        _trajectory_fields={"text": body},\n'
        '    )',
        '    await agent._send("steering_delivered", count=len(drained))',
        "tests/test_steering.py::test_the_delivery_event_carries_the_words",
        "the delivery event carries what was steered, not only that steering "
        "happened; observers should not dig words out of model_input",
    ),
    Mutation(
        "trace-viewer-hides-the-steer", 196, "mini_loop/trace_view.py",
        '        elif etype == "steering_delivered":',
        '        elif etype == "steering_delivered_disabled":',
        "tests/test_trace_view.py::test_steering_renders_as_a_first_class_row",
        "a steering delivery renders as a first-class ledger row at the "
        "position it entered the turn, not a generic payload dump",
    ),
    Mutation(
        "catalog-schemas-never-logged", 197, "mini_loop/agent.py",
        '                self._logged_catalogs.add(catalog.fingerprint)\n'
        '                await self._send(\n'
        '                    "tool_catalog",\n'
        '                    fingerprint=catalog.fingerprint,\n'
        '                    schemas=catalog.schemas(),\n'
        '                )',
        '                self._logged_catalogs.add(catalog.fingerprint)',
        "tests/test_reconstructable_requests.py::test_the_catalog_is_recoverable_by_fingerprint",
        "tool schemas are model-visible input; the log carries each distinct "
        "catalog so a past request can be rebuilt after the catalog changes",
    ),
    Mutation(
        "catalog-relogged-every-round", 197, "mini_loop/agent.py",
        '            if catalog.fingerprint not in self._logged_catalogs:',
        '            if True:',
        "tests/test_reconstructable_requests.py::test_one_event_per_distinct_catalog",
        "an unchanged catalog is written once per fingerprint, not once per "
        "round -- the log stores schemas, not schema spam",
    ),
    Mutation(
        "events-forget-their-epoch", 198, "mini_loop/session.py",
        '            "transcript_epoch": self._transcript_epoch,\n'
        '        }',
        '        }',
        "tests/test_reconstructable_requests.py::test_a_superseded_epoch_request_still_reconstructs",
        "every event names the transcript epoch it belonged to; without the "
        "stamp a pre-compaction request joins against the wrong transcript",
    ),
    Mutation(
        "system-prompt-never-logged", 198, "mini_loop/agent.py",
        '                self._logged_system_hashes.add(system_hash)\n'
        '                await self._send("system_prompt", hash=system_hash, text=system)',
        '                self._logged_system_hashes.add(system_hash)',
        "tests/test_reconstructable_requests.py::test_the_round_trip_is_exact",
        "the dynamic system prompt is model-visible input; one event per "
        "distinct hash keeps requests reconstructable",
    ),
    Mutation(
        "reference-events-dumped-raw", 199, "mini_loop/trace_view.py",
        '        elif etype == "tool_catalog":',
        '        elif etype == "tool_catalog_disabled":',
        "tests/test_trace_view.py::test_reference_events_render_compactly",
        "catalog and system-prompt events are reference data: one compact "
        "ledger line with the payload behind the inspector, never a schema "
        "dump drowning the conversation rows",
    ),
    Mutation(
        "fork-cuts-an-open-turn", 201, "mini_loop/manager.py",
        '        if source.busy:\n'
        '            raise RuntimeError(',
        '        if False:\n'
        '            raise RuntimeError(',
        "tests/test_session_fork.py::test_a_busy_session_refuses_to_fork",
        "a fork is valid only at a completed turn boundary (dsh's "
        "eligibility rule); a mid-turn prefix need not be a valid provider "
        "transcript",
    ),
    Mutation(
        "fork-shares-mutable-rows", 201, "mini_loop/manager.py",
        '            child_agent.messages[:] = _copy.deepcopy(source_agent.messages)',
        '            child_agent.messages[:] = list(source_agent.messages)',
        "tests/test_session_fork.py::test_the_transcripts_share_no_mutable_row",
        "the two transcripts share no mutable row; a shallow copy lets an "
        "edit in one appear in both and trips the digest guard in whichever "
        "flushed first",
    ),
    Mutation(
        "a-stranger-forks-the-session", 201, "mini_loop/server.py",
        # Re-anchored in round 202 when fork_session became async.
        '        _require(request, session_id)\n'
        '        try:\n'
        '            child = await _manager(request).fork_session(session_id)',
        '        try:\n'
        '            child = await _manager(request).fork_session(session_id)',
        "tests/test_session_fork.py::test_fork_over_http_is_owner_scoped",
        "a fork duplicates someone's whole conversation; unscoped, it is "
        "transcript exfiltration as a service",
    ),
    Mutation(
        "fork-leaves-no-trace-in-the-source", 202, "mini_loop/manager.py",
        '            await source.emit({\n'
        '                "type": "session_forked",\n'
        '                "child": child.id,\n'
        '                "message_count": len(source_agent.messages),\n'
        '            })',
        '            pass',
        "tests/test_session_fork.py::test_the_source_stream_records_the_fork",
        "a duplicated conversation says so in its own record; a fork the "
        "source log never mentions is an invisible copy of everything",
    ),
    Mutation(
        "checkpoint-cas-removed", 203, "mini_loop/verified_loop.py",
        '    _require(\n'
        '        patch.base_revision == checkpoint.state_revision,',
        '    _require(\n'
        '        True,',
        "tests/test_verified_loop_contracts.py::test_cas_refuses_a_stale_base_revision",
        "a patch applies only against the exact revision it was proposed "
        "for; without CAS two proposers silently overwrite each other",
    ),
    Mutation(
        "verified-without-a-receipt", 203, "mini_loop/verified_loop.py",
        '            if status == "verified":',
        '            if False:',
        "tests/test_verified_loop_contracts.py::test_verified_is_unreachable_without_a_covering_clean_receipt",
        "unverified never completes: a requirement reaches verified solely "
        "through a clean complete receipt naming it (authority rule 6)",
    ),
    Mutation(
        "shadow-trusts-terminal-status", 204, "mini_loop/verified_shadow.py",
        '    saw_error = any(\n'
        '        event.get("type") == "error"\n'
        '        or (event.get("type") == "stuck" and event.get("halted"))\n'
        '        for event in events\n'
        '    )',
        '    saw_error = any(\n'
        '        event.get("type") == "error"\n'
        '        for event in events\n'
        '    )',
        "tests/test_verified_shadow.py::test_an_errored_run_taints_integrity_and_never_verifies",
        "the shadow gate is stricter than terminal status: a stuck-halted "
        "run returns normally and reads completed, but its typed halt event "
        "taints integrity -- an audit must not verify a run the harness "
        "gave up on",
    ),
    Mutation(
        "shadow-counts-subagent-rounds", 204, "mini_loop/verified_shadow.py",
        '        and event.get("purpose") == "agent_turn"\n'
        '        and not event.get("depth")',
        '        and event.get("purpose") == "agent_turn"',
        "tests/test_verified_shadow.py::test_subagent_rounds_stay_out_of_the_shadow",
        "round plans mirror the parent loop's rounds; a child's model calls "
        "are its own story (round 189's lesson, one layer up)",
    ),
    Mutation(
        "evidence-gate-accepts-dangling-refs", 205, "mini_loop/verified_shadow.py",
        '        dangling = [ref for ref in receipt.evidence_refs if ref not in recorded]',
        '        dangling = []',
        "tests/test_verified_shadow.py::test_dangling_evidence_is_named",
        "an audit citing evidence nobody recorded is indistinguishable from "
        "one citing everything; the coverage gate names every dangling ref",
    ),
]


def _run(selector: str | None) -> int:
    chosen = [m for m in MUTATIONS if not selector or selector in m.name]
    if not chosen:
        print(f"no mutation matches {selector!r}")
        return 2

    survived, missing = [], []
    print(f"verifying {len(chosen)} guard(s)\n")
    for mutation in chosen:
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp) / "repo"
            shutil.copytree(
                ROOT, work,
                ignore=shutil.ignore_patterns(
                    ".git", ".venv", "__pycache__", "*.pyc", ".pytest_cache"
                ),
            )
            target = work / mutation.file
            source = target.read_text()
            occurrences = source.count(mutation.old)
            if occurrences == 0:
                # The code moved. Reporting this is the whole point: a mutation
                # that cannot be applied is a check that silently stops running.
                missing.append(mutation)
                print(f"  STALE     {mutation.name}  (anchor not found in {mutation.file})")
                continue
            if occurrences > 1:
                # `replace(..., 1)` would silently break the *first* match, which
                # need not be the site the named test covers -- exactly the
                # ambiguous-edit defect round 162 fixed in `edit_file`, here in
                # the instrument that checks for such defects. A guard aimed at
                # the wrong site reports "caught" or "SURVIVED" about code nobody
                # asked it to check, so refuse rather than guess.
                missing.append(mutation)
                print(f"  AMBIGUOUS {mutation.name}  (anchor matches {occurrences} "
                      f"places in {mutation.file}; it would mutate the first)")
                continue
            target.write_text(source.replace(mutation.old, mutation.new, 1))

            result = subprocess.run(
                [sys.executable, "-m", "pytest", mutation.test, "-q", "-x", "--no-header"],
                cwd=work, capture_output=True, text=True, timeout=300,
            )
            if result.returncode == 0:
                survived.append(mutation)
                print(f"  SURVIVED  {mutation.name}  ({mutation.test} still passed)")
            else:
                print(f"  caught    {mutation.name}  (r{mutation.round}: {mutation.claim})")

    print()
    if survived or missing:
        for mutation in survived:
            print(f"SURVIVED: {mutation.name} -- {mutation.test} does not pin: {mutation.claim}")
        for mutation in missing:
            print(f"STALE: {mutation.name} -- anchor gone from {mutation.file}")
        return 1
    print(f"all {len(chosen)} guards are load-bearing.")
    return 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("-k", dest="selector", default=None)
    raise SystemExit(_run(parser.parse_args().selector))
