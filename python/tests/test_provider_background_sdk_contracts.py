import json
from tools.provider_background_options_contracts import provider_background_options_contracts
from tools.provider_background_sdk_contracts import provider_background_sdk_contracts


def test_background_sdk_and_managed_optional_stages(tmp_path):
    observed = provider_background_sdk_contracts(tmp_path / 'sdk')
    standalone = {row['name']: row for row in provider_background_options_contracts(tmp_path / 'standalone')['cases']}
    assert observed['sdk_version'] == '0.107.1'
    assert len(observed['cases']) == len(observed['managed']) == 48
    blocked = {prefix + number for prefix in ('timeout-', 'prefix-')
               for number in ('nan', 'infinity', 'negative-infinity')}
    for row in observed['cases']:
        assert row['content_json'] == row['stream_content_json']
        actual = json.loads(row['content_json'])[0]['input']
        assert json.dumps(actual, sort_keys=True) == json.dumps(json.loads(row['input_json']), sort_keys=True)
        if row['name'] in blocked:
            assert row['forward_requests'] == 0
            assert row['forward_error'] == {'class': 'ValueError', 'message': 'Out of range float values are not JSON compliant'}
        else:
            assert row['forward_requests'] == 1 and row['forward_error'] is None
    for row in observed['managed']:
        assert row['error'] is None and row['tool_uses'] == 1 and row['messages'] == 4
        if row['name'] in blocked:
            assert row['trace'] == ['before', 'guard', 'result']
            assert row['transport_requests'] == 1
            assert not row['action_recorded'] and not row['background_initialized']
            assert row['background_counter'] == row['starts_at_settlement'] == 0 and row['task'] is None
            assert row['tool_output'] == 'Error: Out of range float values are not JSON compliant'
            assert row['final'] == '[Error] ValueError: Out of range float values are not JSON compliant'
        else:
            assert row['trace'] == ['before', 'guard', 'after', 'result']
            assert row['transport_requests'] == 2 and row['action_recorded'] and row['background_initialized']
            assert row['background_counter'] == row['starts_at_settlement'] == 1
            assert row['tool_output'] == standalone[row['name']]['output']
            assert row['task'] == {key: standalone[row['name']][key] for key in ('status', 'result')}
            assert row['final'] == 'finished'
