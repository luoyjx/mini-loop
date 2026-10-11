import json
from pathlib import Path

from tools.provider_endpoint_contracts import provider_endpoint_contracts


def test_actual_endpoint_request_paths_match_frozen_contract():
    fixture = provider_endpoint_contracts()
    root = Path(__file__).resolve().parents[2]
    assert fixture == json.loads((root / 'go/testdata/python-provider-endpoint.json').read_text())
    assert len(fixture['cases']) == 40
    for row in fixture['cases']:
        assert len(row['calls']) == 1
        assert row['calls'][0]['method'] == 'POST'
        assert row['calls'][0]['api_key'] == 'fixture-key'
        assert row['text'] == 'ok'
    paths = {(row['base_url'], row['mode']): row['calls'][0]['raw_path'] for row in fixture['cases']}
    assert paths['https://fixture.invalid/tenant//', 'direct'] == '/tenant//v1/messages'
    assert paths['https://fixture.invalid/a/../tenant', 'stream'] == '/tenant/v1/messages'
    assert paths['https://fixture.invalid/tenant/%2e%2E/child', 'direct'] == '/tenant/%2e%2E/child/v1/messages'
