import json
from pathlib import Path

from tools.provider_authority_wire_contracts import provider_authority_wire_contracts


def test_actual_sdk_authority_http_wire_matches_frozen_contract():
    actual = provider_authority_wire_contracts()
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / 'go/testdata/python-provider-authority-wire.json').read_text())
    assert len(actual['cases']) == 14
    for row in actual['cases']:
        assert len(row['requests']) == len(row['dials']) == 1
        assert row['requests'][0]['target'] == '/tenant/v1/messages'
        assert row['requests'][0]['api_key'] == 'fixture-key'
        assert row['text'] == 'ok'
    by_name = {row['name']: row for row in actual['cases'] if row['mode'] == 'direct'}
    assert by_name['unicode-host']['requests'][0]['host'] == 'xn--bcher-kva.invalid'
    assert by_name['sharp-s-host']['dials'][0]['host'] == 'xn--fa-hia.invalid'
    assert by_name['empty-fragment']['requests'][0]['target'] == '/tenant/v1/messages'

    assert by_name['dotted-i-host']['requests'][0]['host'] == 'xn--i-9bb.invalid'
