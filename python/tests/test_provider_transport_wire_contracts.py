import json
from pathlib import Path

from tools.provider_transport_wire_contracts import provider_transport_wire_contracts


def test_actual_proxy_tls_wire_matches_frozen_contract():
    actual = provider_transport_wire_contracts()
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / 'go/testdata/python-provider-transport-wire.json').read_text())
    assert len(actual['cases']) == 24
    for row in actual['cases']:
        assert row['handlers_joined'] and row['writers_closed']
        assert len(row['requests']) == len(row['dials']) == 1
        request = row['requests'][0]
        path = row['base_url'].split('fixture.invalid', 1)[1] + '/v1/messages'
        target = row['base_url'] + '/v1/messages' if row['route'] == 'http-proxy' else path
        assert request == dict(method='POST', target=target, version='HTTP/1.1',
                               host='fixture.invalid', api_key='fixture-key')
        assert row['text'] == 'ok'
        assert row['dials'] == ([dict(host='fixture.invalid', port=443)]
                                if row['route'] == 'https-direct' else
                                [dict(host='127.0.0.1', port='owned-proxy')])
        assert row['snis'] == ([] if row['route'] == 'http-proxy' else ['fixture.invalid'])
        assert row['connects'] == ([] if row['route'] != 'https-proxy' else [
            dict(method='CONNECT', target='fixture.invalid:443', version='HTTP/1.1',
                 host='fixture.invalid:443', api_key=None)])
