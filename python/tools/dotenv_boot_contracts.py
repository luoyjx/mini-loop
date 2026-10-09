"""Observe actual config import, dotenv effects, authentication cleanup and Settings."""

import __main__
import importlib.metadata
import importlib.util
import logging
import os
from pathlib import Path
import sys
from unittest.mock import patch

from tools.dotenv_file_contracts import _without_repl


def dotenv_boot_contracts(scratch: Path) -> dict:
    scratch = scratch.resolve()
    source = (Path(__file__).resolve().parents[1] / "mini_loop/config.py").read_bytes()
    recipes = [
        dict(name="caller-nearest", files={"source/pkg/.env": "MODEL_ID=nearest\n__BOOT_ROUTE=nearest\n"}),
        dict(name="caller-parent", files={"source/.env": "MODEL_ID=parent\n__BOOT_ROUTE=parent\n"}),
        dict(name="root", files={".env": "MODEL_ID=root\n__BOOT_ROUTE=root\n"}),
        dict(name="cwd-ignored", files={"working/.env": "MODEL_ID=cwd\n__BOOT_ROUTE=cwd\n"}),
        dict(name="interactive", interactive=True, files={"working/.env": "MODEL_ID=cwd\n__BOOT_ROUTE=cwd\n"}),
        dict(name="debugger", debugger=True, files={"working/.env": "MODEL_ID=cwd\n__BOOT_ROUTE=cwd\n"}),
        dict(name="frozen", frozen=True, files={"working/.env": "MODEL_ID=cwd\n__BOOT_ROUTE=cwd\n"}),
        dict(name="override-and-interpolate", files={"source/.env": "MODEL_ID=file\nANTHROPIC_API_KEY=file-key\nMINILOOP_MAX_TOKENS=123\nDERIVED=${MODEL_ID}\n"}),
        dict(name="base-url-removes-token", files={"source/.env": "ANTHROPIC_BASE_URL=https://example.invalid\n"}),
        dict(name="empty-base-url-keeps-token", environment={"ANTHROPIC_BASE_URL": "https://incoming.invalid"},
             files={"source/.env": "ANTHROPIC_BASE_URL=\n"}),
        dict(name="whitespace-base-url-removes-token", files={"source/.env": "ANTHROPIC_BASE_URL=' '\n"}),
        dict(name="disabled-invalid-file", environment={"PYTHON_DOTENV_DISABLED": "true"},
             files={"source/pkg/.env": b"MODEL_ID=\xff\n"}),
        dict(name="disabled-base-url-cleanup", environment={"PYTHON_DOTENV_DISABLED": "true", "ANTHROPIC_BASE_URL": "https://incoming.invalid"},
             files={"source/pkg/.env": b"MODEL_ID=\xff\n"}),
        dict(name="invalid-file", files={"source/pkg/.env": b"MODEL_ID=prior\nBAD=\xff\n"}),
        dict(name="partial-import-fault", files={"source/.env": "MODEL_ID=prior\nANTHROPIC_BASE_URL=https://example.invalid\n'bad=key'=value\n"}),
        dict(name="settings-integer-fault", files={"source/.env": "MODEL_ID=file\nMINILOOP_MAX_TOKENS=bad\nANTHROPIC_BASE_URL=https://example.invalid\n"}),
        dict(name="settings-bound-fault", files={"source/.env": "MINILOOP_MAX_TOKENS=0\n"}),
        dict(name="factories-after-import", files={"source/.env": "MODEL_ID=imported\n"},
             postEnvironment={"MODEL_ID": "later", "ANTHROPIC_BASE_URL": "https://later.invalid", "ANTHROPIC_AUTH_TOKEN": "later-token"},
             postFile="MODEL_ID=not-reloaded\n"),
    ]
    rows = []
    for recipe in recipes:
        root = scratch / recipe["name"]
        caller = root / "source/pkg/config.py"
        caller.parent.mkdir(parents=True, exist_ok=True)
        caller.write_bytes(source)
        cwd = root / "working/deep"
        cwd.mkdir(parents=True, exist_ok=True)
        files = {".env": "__BOOT_ROUTE=root\n", **recipe["files"]}
        inputs = []
        for name, content in files.items():
            data = content.encode() if isinstance(content, str) else content
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
            inputs.append(dict(path=name, content=data.hex()))
        environment = dict(MODEL_ID="incoming", ANTHROPIC_API_KEY="incoming-key",
                           ANTHROPIC_AUTH_TOKEN="incoming-token", MINILOOP_FAKE_LLM="1",
                           MINILOOP_WORKSPACE_ROOT=str(root / "workspaces"))
        environment.update(recipe.get("environment", {}))
        module_name = "_mini_loop_dotenv_boot_contract"
        spec = importlib.util.spec_from_file_location(module_name, caller)
        module = importlib.util.module_from_spec(spec)
        previous_module = sys.modules.get(module_name)
        previous_cwd = Path.cwd()
        fault, settings, imported, stage = None, None, False, "import"
        post = recipe.get("postEnvironment", {})
        try:
            sys.modules[module_name] = module
            os.chdir(cwd)
            with patch.dict(os.environ, environment, clear=True), \
                    patch.object(sys, "gettrace", return_value=object() if recipe.get("debugger") else None), \
                    patch.object(sys, "frozen", recipe.get("frozen", False), create=True), \
                    patch.object(__main__, "__file__", str(caller), create=True), \
                    patch.object(logging.getLogger("dotenv.main"), "warning"):
                with patch.object(sys, "ps1", ">>>", create=True) if recipe.get("interactive") else _without_repl():
                    try:
                        spec.loader.exec_module(module)
                        imported = True
                        stage = "settings"
                        os.environ.update(post)
                        if "postFile" in recipe:
                            (root / "source/.env").write_text(recipe["postFile"])
                        cfg = module.load_settings()
                        settings = dict(model=cfg.model, baseURL=cfg.base_url, apiKey=cfg.api_key,
                                        maxTokens=cfg.max_tokens, fakeLLM=cfg.fake_llm,
                                        workspaceRoot=str(cfg.workspace_root))
                        stage = "ready"
                    except (OSError, ValueError) as exc:
                        fault = dict(type=type(exc).__name__, message=str(exc))
                    applied = dict(os.environ)
        finally:
            os.chdir(previous_cwd)
            if previous_module is None:
                sys.modules.pop(module_name, None)
            else:
                sys.modules[module_name] = previous_module
        # Only transient fixture path spelling is normalized; Source execution is intact.
        def normalize(value):
            if isinstance(value, str):
                return value.replace(str(root), "$ROOT")
            if isinstance(value, dict):
                return {key: normalize(item) for key, item in value.items()}
            return value
        rows.append(dict(name=recipe["name"], files=inputs, environment=normalize(environment),
                         cwd="working/deep", caller="source/pkg/config.py",
                         interactive=recipe.get("interactive", False), debugger=recipe.get("debugger", False),
                         frozen=recipe.get("frozen", False), postEnvironment=post,
                         postFile=recipe.get("postFile"), imported=imported, stage=stage,
                         settings=normalize(settings), fault=fault, applied=normalize(applied),
                         workspaceCreated=(root / "workspaces").exists()))
    return dict(version=importlib.metadata.version("python-dotenv"), cases=rows)
