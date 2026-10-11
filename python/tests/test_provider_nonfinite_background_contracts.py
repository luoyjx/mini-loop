import json

from tools.provider_nonfinite_background_contracts import provider_nonfinite_background_contracts


def test_nonfinite_background_sdk_gate_and_managed_stages(tmp_path):
    fixture = provider_nonfinite_background_contracts(tmp_path)
    assert len(fixture['cases']) == 27
    assert len(fixture['managed']) == 18
    for row in fixture['cases']:
        assert json.loads(row['content_json'])[0]['name'] == 'background_run'
        assert row['content_json'] == row['stream_content_json']
        mode = row['mode']
        if mode == 'deny':
            assert row['trace'] == ['before', 'result']
        elif mode == 'guard-deny':
            assert row['trace'] == ['before', 'guard', 'result']
        else:
            assert row['trace'] == ['before', 'guard', 'after', 'result']
        initialized = mode == 'rewrite' or (mode == 'keep' and row['name'].startswith('command'))
        assert row['background_initialized'] == initialized
        assert row['background_counter'] == (1 if mode == 'rewrite' else 0)
        if mode == 'rewrite':
            assert row['output'] == 'Started background task bg_0001: printf background-input-rewritten'
        if mode == 'keep':
            archived = json.loads(row['reopened_messages_json'])[0]['content']
            assert json.dumps(archived, sort_keys=True) == row['content_json']
        if mode in {'deny', 'guard-deny'}:
            assert row['outcomes'][0]['denied']
        elif mode == 'keep':
            assert row['outcomes'][0]['failed']
    for row in fixture['managed']:
        assert row['transport_requests'] == 1
        assert not row['action_recorded']
        assert not row['background_initialized']
        if row['name'].startswith('root'):
            assert row['trace'] == []
            assert row['tool_uses'] == 0
            assert row['messages'] == 2
            assert row['error']['class'] == 'TypeError'
        else:
            assert row['error'] is None
            assert row['trace'] == ['before', 'guard', 'result']
            assert row['tool_uses'] == 1
            assert row['messages'] == 4
            assert row['tool_output'] == 'Error: Out of range float values are not JSON compliant'
