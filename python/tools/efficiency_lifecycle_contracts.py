"""Real efficiency lifecycle ordering, cached reports, admission and draining."""
import asyncio
from mini_loop.token_efficiency import (
    ComponentBinding, ComponentDescriptor, LifecycleHealth, LifecycleStatus,
    MaskedObservation, ObservationReduction, OptimizationMode, StageComponents,
    TokenEfficiencyRuntime,
)


def efficiency_lifecycle_contracts() -> dict:
    def projection(report):
        result = report.as_dict()
        for record in result["components"]:
            record["elapsed_ms"] = 0
        return result

    async def scenario(name, actions, mode="enforce", default="off"):
        events = []
        probes = []
        class Bare:
            def __init__(self, index, action):
                self.action = action
                self.descriptor = ComponentDescriptor(id=f"probe-{index}", version="1", stage="observation", timeout_ms=5 if action == "timeout" else 1000)
            async def reduce(self, observation, *, query=None, budget_tokens=None):
                events.append([self.descriptor.id, "reduce"])
                return ObservationReduction("short")
        class Probe(Bare):
            async def initialize(self, services=None):
                events.append([self.descriptor.id, "initialize"])
                if self.action == "failure": raise RuntimeError("private initialize detail")
                if self.action == "unsafe": raise type("Bad private name", (Exception,), {})("private")
                if self.action == "timeout": await asyncio.sleep(1)
            def health(self):
                events.append([self.descriptor.id, "health"])
                if self.action == "failure": raise RuntimeError("private health detail")
                if self.action == "false": return False
                if self.action == "true": return True
                if self.action == "none": return None
                if self.action == "degraded": return LifecycleHealth("degraded", "capacity_low")
                if self.action == "skipped": return LifecycleHealth("skipped")
                if self.action == "wrong": return "private invalid result"
                if self.action == "async":
                    async def bad(): return None
                    return bad()
                if self.action == "forged":
                    health = LifecycleHealth()
                    object.__setattr__(health, "detail", "private invalid detail")
                    return health
                return LifecycleHealth()
            async def close(self, deadline_seconds=None):
                events.append([self.descriptor.id, "close"])
                if self.action == "failure": raise RuntimeError("private close detail")
                if self.action == "timeout": await asyncio.sleep(1)
        for index, action in enumerate(actions):
            probes.append((Bare if action == "missing" else Probe)(index, action))
        bindings = tuple(ComponentBinding(probe, mode) for probe in probes)
        runtime = TokenEfficiencyRuntime(StageComponents(observation_reducers=bindings), default_mode=default)
        first = await runtime.initialize()
        repeated = await runtime.with_raw_store(None).initialize()
        health = runtime.health()
        closed = await runtime.with_raw_store(None).close()
        closed_again = await runtime.close()
        after_close = await runtime.reduce_observation(MaskedObservation("long observation"))
        receipts = [r.as_dict() for r in after_close.receipts]
        for r in receipts: r["elapsed_ms"] = 0
        return dict(name=name, actions=actions, mode=mode, default=default,
            initialize=projection(first), health=projection(health), close=projection(closed),
            cached_initialize=first is repeated, cached_close=closed is closed_again,
            closed_health=projection(runtime.health()),
            closed_initialize=projection(await runtime.initialize()),
            events=events, closed_receipts=receipts, closed_warnings=list(after_close.warnings))

    async def capture():
        rows=[]
        for name, actions, mode, default in [
            ("empty", [], "enforce", "off"),
            ("ordered", ["ok", "ok"], "enforce", "off"),
            ("fail-open", ["ok", "failure", "ok"], "enforce", "off"),
            ("unsafe-initialize", ["unsafe"], "shadow", "off"),
            ("timeout", ["timeout"], "enforce", "off"),
            ("off", ["ok"], "off", "enforce"),
            ("missing", ["missing"], "enforce", "off"),
            ("health-values", ["false", "true", "none", "degraded", "skipped"], "enforce", "off"),
            ("bad-health", ["wrong", "async", "forged"], "enforce", "off"),
        ]:
            rows.append(await scenario(name, actions, mode, default))
        return rows
    return dict(rows=asyncio.run(capture()))
