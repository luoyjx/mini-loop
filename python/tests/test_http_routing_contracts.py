import json
import re
from pathlib import Path

from tools.http_routing_contracts import http_routing_contracts


def test_actual_routing_contracts_preserve_registration_and_admission_order():
    fixture = http_routing_contracts()
    root = Path(__file__).resolve().parents[2]
    assert fixture == json.loads((root / 'go/testdata/python-http-routing.json').read_text())
    assert len(fixture['routes']) == 40
    assert len(fixture['cases']) == 160
    methods = {route['path']: route['ordered_methods'] for route in fixture['routes']}
    matched = {re.sub(r'\{[^}]+\}', 'fixture-id', pattern): pattern for pattern in methods}
    assert methods['/sessions'] == [['POST'], ['GET']]
    assert methods['/sessions/{session_id}'] == [['GET'], ['DELETE']]
    assert methods['/sessions/{session_id}/cron'] == [['GET'], ['POST']]
    for case in fixture['cases']:
        if case['name'] == 'unsupported-method':
            pattern = matched[case['path']]
            assert case['status'] == 405
            assert case['allowed_methods'] == methods[pattern][0]
        elif case['name'] == 'trailing-slash':
            assert case['status'] == 307
            assert case['location'] == 'http://fixture' + case['path'].replace('/?probe=', '?probe=')
        else:
            assert case['status'] == {'missing-token-slash': 401, 'query-token-slash': 401,
                                      'unknown-path': 404, 'unknown-missing-token': 401,
                                      'oversized-before-redirect': 413}[case['name']]
