"""Offline graph entry for integration tests only; never in langgraph.json."""

import asyncio
import json
from typing import Any

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langgraph_sdk.runtime import ServerRuntime

from mini_loop_deepagents.config import AgentSettings
from mini_loop_deepagents.graph import build_agent


class OfflineToolModel(BaseChatModel):
    """Stateless, prompt-driven model exercises real Deep Agents tool execution."""

    @property
    def _llm_type(self) -> str:
        return "offline-integration-fixture"

    def bind_tools(self, tools, **kwargs):
        return self

    async def _agenerate(self, messages, stop=None, run_manager=None, **kwargs):
        if isinstance(messages[-1], HumanMessage) and str(messages[-1].content).startswith("WAIT:"):
            await asyncio.sleep(float(str(messages[-1].content)[5:]))
        return self._generate(messages, stop=stop, **kwargs)

    def _generate(self, messages, stop=None, run_manager=None, **kwargs: Any):
        if isinstance(messages[-1], ToolMessage):
            response = AIMessage(content="TOOL_RESULT: " + str(messages[-1].content))
        else:
            prompt = next(m.content for m in reversed(messages) if isinstance(m, HumanMessage))
            if isinstance(prompt, str) and prompt.startswith("TOOL:"):
                action = json.loads(prompt[5:])
                response = AIMessage(content="", tool_calls=[{
                    "name": action["name"], "args": action["args"], "id": "offline-call",
                }])
            else:
                response = AIMessage(content="OFFLINE: " + str(prompt))
        return ChatResult(generations=[ChatGeneration(message=response)])


def agent(runtime: ServerRuntime):
    runtime.ensure_user()
    return build_agent(AgentSettings(model="openai:offline"), model=OfflineToolModel())
