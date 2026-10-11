import hashlib
import json
from pathlib import Path

from tools.api_documentation_contracts import api_documentation_contracts, documentation_assets


def test_actual_documentation_admission_and_assets_match_source_contract():
    fixture = api_documentation_contracts()
    assert len(fixture['cases']) == 36
    assets = documentation_assets()
    assert len(assets) == 4
    for asset in fixture['assets']:
        body = assets[asset['filename']]
        assert len(body) == asset['bytes']
        assert hashlib.sha256(body).hexdigest() == asset['sha256']
    root = Path(__file__).resolve().parents[2]
    assert json.loads(assets['openapi.json']) == json.loads((root / 'go/testdata/python-openapi.json').read_text())
    for row in fixture['cases']:
        expected = {'anonymous-get': 200, 'anonymous-head': 200, 'token-get': 200,
                    'missing-token': 401, 'missing-token-head': 401, 'query-token': 401,
                    'unsupported-method': 405, 'trailing-slash': 307, 'oversized-body': 413}
        assert row['status'] == expected[row['name']]
        if row['name'] == 'unsupported-method':
            assert row['allowed_methods'] == ['GET', 'HEAD']
    assert fixture == json.loads((root / 'go/testdata/python-api-documentation.json').read_text())
