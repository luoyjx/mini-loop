"""`--dump-config` prints what actually boots, with credentials redacted.

DeepSeek Harness's `--dump-config` exists so an operator debugs the real
composition, not the one they believe they configured. Pinned here: the
dump reads seams off the same probe technique `posture()` uses, and any
credential-shaped setting shows presence, never its value -- while
non-credential names that merely contain "token" stay readable.
"""

import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.identity import dump_config

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def test_the_dump_covers_settings_harness_tools_and_posture(tmp_path):
    settings = Settings(
        fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS,
        api_key="sk-ant-verysecret", spill_dir=None,
    )
    manager = SessionManager(settings, FakeAsyncAnthropic())
    dump = dump_config(manager, settings)

    assert dump["settings"]["api_key"] == "<set>"
    assert "verysecret" not in str(dump)
    # A budget/mode that merely contains "token" is data, not a credential.
    assert dump["settings"]["token_threshold"] == settings.token_threshold
    assert isinstance(dump["tools"], list) and "bash" in dump["tools"]
    assert "secrets" in dump["harness"]
    assert "sandbox" in dump["posture"]
