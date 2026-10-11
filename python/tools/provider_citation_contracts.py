"""Pinned SDK citation normalization and forwarding, using only mock HTTP."""
from __future__ import annotations

import asyncio
import hashlib
from importlib.metadata import version
import inspect
import json
import warnings

from anthropic import AsyncAnthropic, _models
from anthropic.lib.streaming import _messages
import httpx

from mini_loop.agent import _content_payload

SDK_VERSION = "0.107.1"


def _citations() -> list[dict]:
    document = dict(cited_text="quote 📎 fixture-secret", document_index=0,
                    document_title="标题 fixture-secret", file_id=None)
    return [
        dict(document, type="char_location", start_char_index=0, end_char_index=5),
        dict(document, type="page_location", start_page_number=1, end_page_number=2),
        dict(document, type="content_block_location", start_block_index=0, end_block_index=1),
        dict(type="web_search_result_location", cited_text="web fixture-secret",
             encrypted_index="opaque fixture-secret", title=None,
             url="https://example.invalid/fixture-secret"),
        dict(type="search_result_location", cited_text="search fixture-secret",
             search_result_index=0, start_block_index=0, end_block_index=1,
             source="local fixture-secret", title="search title"),
    ]


def _reply(blocks: list[dict]) -> dict:
    return dict(id="source-citation", type="message", role="assistant",
                model="citation-model", content=blocks, stop_reason="end_turn",
                stop_sequence=None, usage=dict(input_tokens=1, output_tokens=2))


async def _completed(name: str, block: dict) -> dict:
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.host == "source.invalid"
        requests.append(json.loads(request.content))
        return httpx.Response(200, json=_reply([block]))

    transport = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    async with AsyncAnthropic(api_key="fixture-key", auth_token="", max_retries=0,
                             base_url="https://source.invalid", http_client=transport) as client:
        with warnings.catch_warnings(record=True) as caught:
            warnings.simplefilter("always")
            result = await client.messages.create(model="citation-model", max_tokens=8,
                                                  messages=[dict(role="user", content="probe")])
            payload = _content_payload(result.content)
            await client.messages.create(model="citation-model", max_tokens=8,
                                         messages=[dict(role="user", content="probe"),
                                                   dict(role="assistant", content=payload),
                                                   dict(role="user", content="next")])
    return dict(name=name, input=block, normalized=payload[0],
                forwarded=requests[1]["messages"][1]["content"][0],
                requests=len(requests), warnings=sorted({type(w.message).__name__ for w in caught}))


async def _stream(name: str, events: list[dict]) -> dict:
    wire = "".join(f"event: {event['type']}\ndata: {json.dumps(event, ensure_ascii=False)}\n\n" for event in events)

    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.host == "source.invalid"
        body = json.loads(request.content)
        requests.append(body)
        if body.get("stream"):
            return httpx.Response(200, content=wire.encode(), headers={"content-type": "text/event-stream"})
        return httpx.Response(200, json=_reply([dict(type="text", text="next")]))

    observed = []
    transport = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    async with AsyncAnthropic(api_key="fixture-key", auth_token="", max_retries=0,
                             base_url="https://source.invalid", http_client=transport) as client:
        async with client.messages.stream(model="citation-model", max_tokens=8,
                                          messages=[dict(role="user", content="probe")]) as stream:
            async for event in stream:
                if event.type == "text":
                    observed.append(dict(type="text", text=event.text))
                elif event.type == "citation":
                    observed.append(dict(type="citation", citation=event.citation.model_dump()))
            result = await stream.get_final_message()
        content = _content_payload(result.content)
        await client.messages.create(model="citation-model", max_tokens=8,
                                     messages=[dict(role="user", content="probe"),
                                               dict(role="assistant", content=content),
                                               dict(role="user", content="next")])
    return dict(name=name, events=events, content=content, observed=observed,
                forwarded=requests[1]["messages"][1]["content"], requests=len(requests))


def _events(initial: dict, deltas: list[dict]) -> list[dict]:
    start = _reply([])
    start["stop_reason"] = None
    events = [dict(type="message_start", message=start),
              dict(type="content_block_start", index=0, content_block=initial),
              dict(type="content_block_delta", index=0, delta=dict(type="text_delta", text=" tail"))]
    events.extend(dict(type="content_block_delta", index=0,
                       delta=dict(type="citations_delta", citation=c)) for c in deltas)
    events.extend([dict(type="content_block_stop", index=0),
                   dict(type="message_delta", delta=dict(stop_reason="end_turn", stop_sequence=None),
                        usage=dict(output_tokens=2)), dict(type="message_stop")])
    return events


async def _capture() -> dict:
    citations = _citations()
    cases = [(c["type"], dict(type="text", text="answer", citations=[c])) for c in citations]
    cases.extend((name, dict(type="text", text="answer", **fields)) for name, fields in [
        ("absent", {}), ("null", {"citations": None}), ("empty", {"citations": []}),
        ("all_variants", {"citations": citations}),
        ("negative_indices", {"citations": [dict(citations[0], document_index=-1, start_char_index=-2, end_char_index=-1)]}),
        ("unbounded_index", {"citations": [dict(citations[0], document_index=10**80)]}),
        ("extra_members", {"citations": [dict(citations[0], extension={"secret-key": ["fixture-secret", 3]})]}),
        ("unknown_variant", {"citations": [dict(type="future_location", extension=["fixture-secret", None])]}),
        ("wrong_known_field", {"citations": [dict(citations[0], cited_text=7)]}),
        ("missing_known_field", {"citations": [dict(type="char_location", cited_text="partial")]}),
        ("non_array", {"citations": "opaque fixture-secret"}),
        ("null_item", {"citations": [None]}),
    ])
    additional = [
        ("numeric_string", dict(citations[0], document_index="2")),
        ("float_integer", dict(citations[0], document_index=2.0)),
        ("fractional_index", dict(citations[0], document_index=2.5)),
        ("boolean_index", dict(citations[0], document_index=True)),
        ("nullable_absent", {k: v for k, v in citations[0].items() if k not in ("file_id", "document_title")}),
        ("empty_object", {}), ("scalar_item", 7),
        ("nested_items", [dict(citations[0])]),
        ("invalid_discriminator", dict(citations[0], type=7)),
    ]
    additional.extend(("index_text_" + str(i), dict(citations[0], document_index=value))
                      for i, value in enumerate(["2.0", "+2", " 2 ", "1e2", "1_000", "01", "-0.0", "2.00", "2e0"]))
    cases.extend((name, dict(type="text", text="answer", citations=[citation]))
                 for name, citation in additional)
    completed = [await _completed(name, block) for name, block in cases]
    streams = [await _stream("seed_plus_all_deltas", _events(dict(type="text", text="prefix", citations=[citations[0]]), citations)),
               await _stream("null_then_deltas", _events(dict(type="text", text="prefix", citations=None), citations))]
    return dict(sdk_version=SDK_VERSION, pydantic_version=version("pydantic"),
                model_constructor_sha256=hashlib.sha256(inspect.getsource(_models.construct_type).encode()).hexdigest(),
                accumulator_sha256=hashlib.sha256(inspect.getsource(_messages.accumulate_event).encode()).hexdigest(),
                completed=completed, streams=streams)


def provider_citation_contracts() -> dict:
    if version("anthropic") != SDK_VERSION:
        raise RuntimeError(f"citation Source capture requires anthropic {SDK_VERSION}")
    return asyncio.run(_capture())
