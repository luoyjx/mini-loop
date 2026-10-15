"""Side-effect-free graph construction for the standard Agent Server."""

from importlib.resources import files
from typing import Any

from deepagents import create_deep_agent
from deepagents.backends import CompositeBackend, StateBackend, StoreBackend
from langchain_core.language_models import BaseChatModel
from langchain_openai import ChatOpenAI
from langgraph.runtime import Runtime
from langgraph_sdk.runtime import ServerRuntime

from mini_loop_deepagents.config import AgentSettings

SYSTEM_PROMPT = files("mini_loop_deepagents").joinpath("instructions.md").read_text(encoding="utf-8")


def memory_namespace(runtime: Runtime) -> tuple[str, str]:
    """Only the server's verified user selects the cross-thread memory scope."""
    info = runtime.server_info if runtime is not None else None
    user = info.user if info is not None else None
    if user is None or not user.is_authenticated or not user.identity:
        raise PermissionError("authenticated server user required for memory")
    return (user.identity, "memories")


def build_agent(
    settings: AgentSettings,
    *,
    model: BaseChatModel | None = None,
    checkpointer: Any = None,
    store: Any = None,
):
    """Build a real Deep Agent; explicit test embeddings may supply their stores.

    The serving factory supplies neither checkpointer nor store: Agent Server
    owns these resources and thread serialization. No host-filesystem backend or
    shell executor is installed. Deep Agents supplies planning and subagents.
    """
    if model is None:
        model = ChatOpenAI(
            model=settings.model.removeprefix("openai:"),
            max_tokens=settings.max_tokens,
            timeout=settings.timeout_seconds,
            max_retries=2,
        )
    backend = CompositeBackend(
        default=StateBackend(),
        routes={"/memories/": StoreBackend(namespace=memory_namespace)},
    )
    return create_deep_agent(
        model=model,
        system_prompt=SYSTEM_PROMPT,
        backend=backend,
        memory=["/memories/AGENTS.md"],
        checkpointer=checkpointer,
        store=store,
        name="mini_loop_deepagents",
    )


def agent(runtime: ServerRuntime):
    """Factory may run for schemas/state reads as well as Run execution.

    Do not create resources or depend on execution_runtime here. Runtime user
    identity is resolved by the backend at tool execution, including subagents.
    """
    runtime.ensure_user()
    return build_agent(AgentSettings.from_env())
