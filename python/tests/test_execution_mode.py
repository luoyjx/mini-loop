"""Execution mode is a property of the call, not only of the tool.

DeepSeek Harness classifies each tool call as `parallel` or `exclusive`
through `executionMode(input)` -- a function of the arguments -- because a
static flag cannot express a tool whose safety depends on what it was asked
to do. The concrete case here: `bash` with `run_in_background=true` only
enqueues a background task and returns, yet the static `parallel_safe=False`
made it an ordering barrier that serialized the whole batch around an
operation that finishes in microseconds.

Pinned:
* the default classification still follows `parallel_safe`;
* a `mode_for` classifier sees the call and overrides the flag;
* a classifier that throws or answers nonsense degrades to `exclusive`
  (a barrier cannot lose an update; a wrongly-parallel write can);
* `bash` is parallel exactly when backgrounding is BOTH requested and
  available -- without `background_run` installed the flag is ignored and
  the command runs foreground, where parallel would race real writes.
"""

from mini_loop.builtins import default_registry, full_registry
from mini_loop.registry import Tool, ToolCall


def _call(name="t", **input_):
    return ToolCall(name=name, input=dict(input_), id="c1")


def _tool(**over):
    fields = dict(
        name="t", description="d",
        input_schema={"type": "object", "properties": {}},
        handler=lambda ctx: "ok",
    )
    fields.update(over)
    return Tool(**fields)


def test_the_default_follows_the_static_flag():
    assert _tool(parallel_safe=True).execution_mode(_call()) == "parallel"
    assert _tool(parallel_safe=False).execution_mode(_call()) == "exclusive"


def test_a_classifier_sees_the_call_and_overrides_the_flag():
    tool = _tool(
        parallel_safe=False,
        mode_for=lambda call: "parallel" if call.input.get("bg") else "exclusive",
    )
    assert tool.execution_mode(_call(bg=True)) == "parallel"
    assert tool.execution_mode(_call()) == "exclusive"


def test_a_broken_classifier_degrades_to_a_barrier():
    assert _tool(mode_for=lambda call: 1 / 0).execution_mode(_call()) == "exclusive"
    assert _tool(mode_for=lambda call: "yolo").execution_mode(_call()) == "exclusive"


def test_background_bash_is_parallel_only_when_backgrounding_exists():
    with_bg = full_registry(background=True).get("bash")
    assert with_bg.execution_mode(_call("bash", command="sleep 99", run_in_background=True)) == "parallel"
    assert with_bg.execution_mode(_call("bash", command="ls")) == "exclusive"

    # No background_run tool -> the flag is ignored at dispatch, so the
    # classification must not promise parallelism the dispatch cannot honor.
    without_bg = default_registry().get("bash")
    assert without_bg.execution_mode(_call("bash", command="sleep 99", run_in_background=True)) == "exclusive"
