"""Actual stage/lifecycle behavior when arbitrary-size timeout milliseconds overflow."""

def efficiency_timeout_contracts() -> dict:
    import asyncio
    from mini_loop.token_efficiency import (
        ComponentDescriptor, MaskedObservation, ObservationReduction, RequestContext,
        RequestOptimization, ResponsePolicyContext, ResponsePolicyPlan,
        TokenEfficiencyRegistry,
    )

    async def collect():
        rows = []
        for timeout in (1, 2**63, 2**1024, 10**312, 10**400):
            for operation in ("observation", "request", "response", "initialize", "close"):
                events = []
                class Ready:
                    def __init__(self, value, event):
                        self.value, self.event = value, event
                    def __await__(self):
                        events.append(self.event)
                        future = asyncio.get_running_loop().create_future()
                        future.set_result(self.value)
                        return future.__await__()
                class Probe:
                    descriptor = ComponentDescriptor(id="timeout-probe", version="1",
                        stage="observation", timeout_ms=timeout)
                    def reduce(self, observation, **kwargs):
                        return Ready(ObservationReduction(observation.content), "reduce")
                    def optimize(self, context, **kwargs):
                        return Ready(RequestOptimization(context.request), "optimize")
                    def plan(self, context):
                        return Ready(ResponsePolicyPlan(context.settings), "plan")
                    async def initialize(self, services=None):
                        events.append("initialize")
                    async def close(self, deadline_seconds=None):
                        events.append("close")
                probe = Probe()
                registry = TokenEfficiencyRegistry()
                if operation == "request":
                    probe.descriptor = ComponentDescriptor(id="timeout-probe", version="1",
                        stage="request_context", timeout_ms=timeout)
                    registry.register_request_optimizer(probe)
                elif operation == "response":
                    probe.descriptor = ComponentDescriptor(id="timeout-probe", version="1",
                        stage="response_policy", timeout_ms=timeout)
                    registry.register_response_policy(probe)
                else:
                    registry.register_observation(probe)
                runtime = registry.runtime(default_mode="enforce")
                records, error = [], None
                try:
                    if operation == "observation":
                        result = await runtime.reduce_observation(MaskedObservation("text"))
                    elif operation == "request":
                        result = await runtime.optimize_request(RequestContext({"messages":[]}))
                    elif operation == "response":
                        result = await runtime.plan_response(ResponsePolicyContext("task"))
                    elif operation == "initialize":
                        result = await runtime.initialize()
                    else:
                        result = await runtime.close()
                    items = result.receipts if operation in {"observation","request","response"} else result.components
                    records = [dict(status=item.status.value, reason=item.reason if operation in {"observation","request","response"} else item.detail) for item in items]
                except Exception as exc:
                    error = dict(kind=type(exc).__name__, message=str(exc))
                row = dict(timeout_ms=timeout, operation=operation, events=events,
                           records=records, error=error)
                if operation == "close":
                    denied = await runtime.reduce_observation(MaskedObservation("text"))
                    row["admission_reason"] = denied.receipts[0].reason
                rows.append(row)
        return dict(rows=rows)
    return asyncio.run(collect())
