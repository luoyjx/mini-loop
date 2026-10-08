"""Real Source component descriptors and event-safe optimization receipts."""
from copy import deepcopy
from dataclasses import asdict


def efficiency_type_contracts() -> dict:
    from mini_loop.token_efficiency import ComponentDescriptor, OptimizationReceipt

    descriptor_base = dict(id="example.reducer", version="1.2+3", stage="observation",
        content_types=["text/plain"], deterministic=True, lossiness="lossless",
        recoverable=False, cost_tier="fast", network_access=False, timeout_ms=1000,
        max_input_bytes=None, capabilities=["logs", "logs", "glob"])
    receipt_base = dict(component_id="example.reducer", component_version="1.2+3",
        stage="observation", mode="enforce", status="applied", reason="reduced:2",
        raw_bytes=100, projected_bytes=80, tokens_before_estimate=25,
        tokens_after_estimate=20, input_digest="sha256:" + "a" * 64,
        output_digest="sha256:" + "b" * 64, lossiness="recoverable",
        deterministic=True, raw_ref="raw_" + "x" * 43, raw_digest="sha256:" + "a" * 64,
        warnings=["trimmed:2", "trimmed:2"], elapsed_ms=1.5)
    descriptor_specs = [
        ("default", {}), ("id-max", dict(id="a" * 128)),
        ("id-too-long", dict(id="a" * 129)), ("id-escape", dict(id="../escape")),
        ("id-newline", dict(id="safe\n")), ("version-max", dict(version="1" * 64)),
        ("version-too-long", dict(version="1" * 65)),
        ("version-newline", dict(version="safe\n")),
        ("request-stage", dict(stage="request_context")),
        ("response-stage", dict(stage="response_policy")),
        ("bad-stage", dict(stage="bogus")), ("bad-lossiness", dict(lossiness="bogus")),
        ("no-content-types", dict(content_types=[])),
        ("bad-content-type", dict(content_types=["text"])),
        ("slash-content-type", dict(content_types=["/"])),
        ("empty-capability", dict(capabilities=[""])),
        ("zero-timeout", dict(timeout_ms=0)), ("negative-timeout", dict(timeout_ms=-1)),
        ("large-timeout", dict(timeout_ms=10**100)),
        ("zero-input-limit", dict(max_input_bytes=0)),
        ("large-input-limit", dict(max_input_bytes=10**100)),
        ("cost-ml", dict(cost_tier="ml")), ("cost-remote", dict(cost_tier="remote")),
        ("bad-cost", dict(cost_tier="other")),
        ("unbacked-recoverable", dict(lossiness="recoverable")),
        ("backed-recoverable", dict(lossiness="recoverable", recoverable=True)),
        ("lossy", dict(lossiness="lossy")),
    ]
    receipt_specs = [
        ("changed", {}), ("unchanged", dict(output_digest=receipt_base["input_digest"])),
        ("no-raw-ref", dict(raw_ref=None, raw_digest=None)),
        ("bad-id", dict(component_id="../escape")),
        ("bad-version", dict(component_version="secret\nvalue")),
        ("bad-reason", dict(reason="contains free text")),
        ("reason-max", dict(reason="A" * 128 + ":" + "b" * 128)),
        ("reason-overlong", dict(reason="A" * 129)),
        ("bad-stage", dict(stage="bogus")), ("bad-mode", dict(mode="bogus")),
        ("bad-status", dict(status="bogus")), ("bad-lossiness", dict(lossiness="bogus")),
        ("warnings-max", dict(warnings=["trimmed:2"] * 32)),
        ("warnings-overflow", dict(warnings=["trimmed:2"] * 33)),
        ("warning-overlong", dict(warnings=["w" * 257])),
        ("warning-unsafe", dict(warnings=["free text"])),
        ("warning-uppercase", dict(warnings=["Uppercase"])),
        ("warning-numeric-suffix-max", dict(warnings=["trimmed:123456789012"])),
        ("warning-numeric-suffix-overflow", dict(warnings=["trimmed:1234567890123"])),
        ("negative-elapsed", dict(elapsed_ms=-1)),
        ("bad-input-digest", dict(input_digest="sha256:" + "A" * 64)),
        ("bad-output-digest", dict(output_digest="bad")),
        ("unpaired-raw-ref", dict(raw_digest=None)),
        ("unpaired-raw-digest", dict(raw_ref=None)),
        ("bad-raw-ref", dict(raw_ref="raw_" + "x" * 42)),
        ("bad-raw-digest", dict(raw_digest="bad")),
        ("large-counter", dict(raw_bytes=10**1000)),
    ]
    receipt_specs += [("negative-" + field, {field: -1}) for field in (
        "raw_bytes", "projected_bytes", "tokens_before_estimate", "tokens_after_estimate")]
    receipt_specs += [("mode-" + mode, dict(mode=mode)) for mode in ("off", "shadow")]
    receipt_specs += [("status-" + status, dict(status=status)) for status in (
        "passthrough", "shadowed", "degraded", "error")]
    receipt_specs += [("stage-" + stage, dict(stage=stage)) for stage in ("request_context", "response_policy")]
    receipt_specs += [("lossiness-" + lossiness, dict(lossiness=lossiness)) for lossiness in ("lossless", "lossy")]

    def capture(base, specs, constructor, receipt=False):
        rows = []
        for name, changes in specs:
            value = deepcopy(base)
            value.update(changes)
            params = deepcopy(value)
            if receipt:
                params["warnings"] = tuple(params["warnings"])
            try:
                result = constructor(**params)
                output = result.as_dict() if receipt else asdict(result)
                error = None
            except (TypeError, ValueError) as fault:
                output, error = None, dict(kind=type(fault).__name__, message=str(fault))
            rows.append(dict(name=name, input=value, output=output, error=error))
        return rows

    return dict(descriptors=capture(descriptor_base, descriptor_specs, ComponentDescriptor),
        receipts=capture(receipt_base, receipt_specs, OptimizationReceipt, receipt=True))
