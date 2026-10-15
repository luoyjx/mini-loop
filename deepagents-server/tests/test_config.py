import pytest

from mini_loop_deepagents.config import AgentSettings, load_tokens


@pytest.mark.parametrize("value", ["", "{}", "[]", '"secret"', '{"short":"alice"}', '{"' + "x"*32 + '":"../alice"}', '{"' + "x"*32 + '":"alice","' + "x"*32 + '":"bob"}'])
def test_bad_token_configuration_fails_without_echoing_secrets(value):
    with pytest.raises(ValueError) as error:
        load_tokens({"MINILOOP_DEEPAGENTS_TOKENS": value})
    assert value not in str(error.value) or not value


def test_configuration_is_deployment_owned():
    assert load_tokens({"MINILOOP_DEEPAGENTS_TOKENS": '{"' + "x"*32 + '":"alice"}'}) == {"x"*32: "alice"}
    assert AgentSettings.from_env({}).model == "openai:gpt-4.1-mini"


@pytest.mark.parametrize("key,value", [("TIMEOUT_SECONDS", "nan"), ("TIMEOUT_SECONDS", "inf"), ("TIMEOUT_SECONDS", "0"), ("MAX_TOKENS", "0"), ("MAX_TOKENS", "32769"), ("MODEL", "openai:"), ("MODEL", "untrusted:model")])
def test_invalid_model_limits(key, value):
    with pytest.raises(ValueError):
        AgentSettings.from_env({"MINILOOP_DEEPAGENTS_" + key: value})
