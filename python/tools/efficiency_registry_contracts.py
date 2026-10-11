"""Actual Source registry ordering, snapshots and shared multi-stage lifecycle."""

import asyncio

from mini_loop.token_efficiency import (
    ComponentBinding, ComponentDescriptor, LifecycleHealth, MaskedObservation,
    ObservationReduction, RequestContext, RequestOptimization, ResponsePolicyContext,
    ResponsePolicyPlan, StageComponents, TokenEfficiencyRegistry, TokenEfficiencyRuntime,
)


def efficiency_registry_contracts() -> dict:
    def project(report):
        result = report.as_dict()
        for item in result["components"]:
            item["elapsed_ms"] = 0
        return result

    class Probe:
        def __init__(self, identity, stage, events):
            self.descriptor = ComponentDescriptor(id=identity, version="1", stage=stage)
            self.events = events
        async def initialize(self, services=None):
            self.events.append([self.descriptor.id, "initialize"])
        def health(self):
            self.events.append([self.descriptor.id, "health"])
            return LifecycleHealth()
        async def close(self, deadline_seconds=None):
            self.events.append([self.descriptor.id, "close"])
        async def reduce(self, observation, *, query=None, budget_tokens=None):
            self.events.append([self.descriptor.id, "reduce"])
            return ObservationReduction(observation.text)
        async def optimize(self, context, *, budget_tokens=None):
            self.events.append([self.descriptor.id, "optimize"])
            return RequestOptimization(context.request)
        async def plan(self, context):
            self.events.append([self.descriptor.id, "plan"])
            return ResponsePolicyPlan(context.settings)

    async def scenario(name, entries, *, direct=False, default="enforce"):
        events, errors, identities = [], [], {}
        registry = TokenEfficiencyRegistry()
        targets = {stage: [] for stage in ("observation", "request_context", "response_policy")}
        accepted = []
        for entry in entries:
            probe = identities.setdefault(entry["object"], Probe(entry["id"], entry["descriptor_stage"], events))
            try:
                if direct:
                    targets[entry["target"]].append(ComponentBinding(probe, entry["mode"]))
                else:
                    register = {"observation": registry.register_observation,
                                "request_context": registry.register_request_optimizer,
                                "response_policy": registry.register_response_policy}[entry["target"]]
                    assert register(probe, mode=entry["mode"]) is probe
                accepted.append(entry["id"])
            except (TypeError, ValueError) as error:
                errors.append(str(error))
        stages = (StageComponents(tuple(targets["observation"]), tuple(targets["request_context"]),
                                  tuple(targets["response_policy"])) if direct else registry.snapshot())
        snapshot = [[binding.descriptor.id for binding in stage] for stage in
                    (stages.observation_reducers, stages.request_optimizers, stages.response_policies)]
        if not direct:
            registry.register_observation(Probe("later", "observation", events))
        runtime = TokenEfficiencyRuntime(stages, default_mode=default)
        clone = runtime.with_raw_store(None)
        initial = await runtime.initialize()
        repeated = await clone.initialize()
        health = clone.health()
        await runtime.reduce_observation(MaskedObservation("text"))
        await clone.optimize_request(RequestContext({"messages": []}))
        await runtime.plan_response(ResponsePolicyContext("task"))
        closed = await clone.close()
        cached_close = closed is await runtime.close()
        return dict(name=name, entries=entries, direct=direct, default=default,
                    accepted=accepted, errors=errors, snapshot=snapshot,
                    initialize=project(initial), health=project(health), close=project(closed),
                    cached_initialize=initial is repeated, cached_close=cached_close, events=events)

    def entry(identity, target, mode=None, *, object_id=None, descriptor_stage=None):
        return dict(id=identity, target=target, mode=mode, object=object_id or identity,
                    descriptor_stage=descriptor_stage or target)

    async def capture():
        ordered = [entry("response", "response_policy"), entry("observation", "observation"), entry("request", "request_context")]
        return [
            await scenario("empty", []),
            await scenario("stage-order", ordered),
            await scenario("default-off", ordered, default="off"),
            await scenario("override-modes", [entry("response", "response_policy", "shadow"), entry("off", "observation", "off"), entry("request", "request_context", "enforce")], default="off"),
            await scenario("global-duplicate", [entry("same", "observation"), entry("same", "response_policy", object_id="second"), entry("request", "request_context")]),
            await scenario("wrong-stage-before-duplicate", [entry("same", "observation"), entry("same", "response_policy", object_id="second", descriptor_stage="observation")]),
            await scenario("invalid-mode", [entry("bad", "observation", "invalid")]),
            await scenario("direct-shared", [entry("same", "observation"), entry("same", "request_context", descriptor_stage="observation"), entry("same", "response_policy", descriptor_stage="observation")], direct=True),
            await scenario("direct-off-first", [entry("same", "observation", "off"), entry("same", "request_context", "enforce", descriptor_stage="observation"), entry("same", "response_policy", "shadow", descriptor_stage="observation")], direct=True),
            await scenario("direct-distinct-same-id", [entry("same", "observation"), entry("same", "observation", object_id="second")], direct=True),
        ]
    return dict(rows=asyncio.run(capture()))
