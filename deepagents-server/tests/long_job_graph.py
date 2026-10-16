"""Deterministic long-job fault fixture; never registered by production config."""

import asyncio
import json
import os
from pathlib import Path
import re

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langgraph_sdk.runtime import ServerRuntime

from mini_loop_deepagents.config import AgentSettings
from mini_loop_deepagents.graph import build_agent


class LongJobModel(BaseChatModel):
    @property
    def _llm_type(self):
        return "controlled-long-job-fixture"

    def bind_tools(self, tools, **kwargs):
        return self

    def _generate(self, *args, **kwargs):
        raise RuntimeError("This fixture requires async execution")

    async def _agenerate(self, messages, stop=None, run_manager=None, **kwargs):
        prompt = next(m.content for m in reversed(messages) if isinstance(m, HumanMessage))
        spec = json.loads(prompt.removeprefix("JOB:"))
        name = spec["name"]
        if not re.fullmatch(r"[a-z0-9_-]{1,40}", name):
            raise ValueError("Invalid fixture job name")
        if isinstance(messages[-1], ToolMessage):
            control = Path(os.environ["MINILOOP_DEEPAGENTS_TEST_CONTROL_DIR"])
            released = lambda: (control / ("release-" + name)).is_file()
            if spec.get("fail") and not await asyncio.to_thread(released):
                raise RuntimeError("intentional model failure after completed tool checkpoint")
            if spec.get("block"):
                while not await asyncio.to_thread(released):
                    await asyncio.sleep(0.05)
            message = AIMessage(content="DONE: " + name)
        else:
            message = AIMessage(content="", tool_calls=[{
                "name": "write_file", "id": "progress-" + name,
                "args": {"file_path": "/progress-" + name + ".txt", "content": "checkpoint:" + name},
            }])
        return ChatResult(generations=[ChatGeneration(message=message)])


def agent(runtime: ServerRuntime):
    runtime.ensure_user()
    return build_agent(AgentSettings(model="openai:long-job-fixture"), model=LongJobModel())
