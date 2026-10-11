import json
from pathlib import Path

from tools.provider_url_contracts import provider_url_contracts


def test_actual_provider_url_scope_matches_frozen_contract():
    observed = provider_url_contracts()
    root = Path(__file__).resolve().parents[2]
    assert observed == json.loads((root / 'go/testdata/python-provider-url.json').read_text())
    assert observed['sdk_version'] == '0.107.1'
    assert observed['httpx_version'] == '0.28.1'
    assert len(observed['cases']) == 68
    rows = {(row['name'], row['mode']): row for row in observed['cases']}
    assert rows['https-default-port', 'direct']['calls'][0]['url'] == 'https://fixture.invalid/tenant/v1/messages'
    assert rows['unicode-host', 'stream']['calls'][0]['url'] == 'https://xn--bcher-kva.invalid/tenant/v1/messages'
    assert rows['invalid-port', 'direct']['stage'] == 'constructor'
    assert rows['relative-path', 'stream']['stage'] == 'request'
    for row in observed['cases']:
        if row['stage'] == 'constructor':
            assert row['calls'] == []
        if row['stage'] == 'complete':
            assert row['text'] == 'ok' and len(row['calls']) == 1
