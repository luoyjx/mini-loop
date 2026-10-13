"""Required Source contract families for the new typed Go public seams."""
import json
from pathlib import Path

from tools.public_helper_contracts import public_helper_contracts


def test_actual_public_helper_source_effects():
    snapshot = public_helper_contracts()
    root = Path(__file__).resolve().parents[2]
    assert snapshot == json.loads((root/'go/testdata/python-public-helpers.json').read_text())
    assert len(snapshot['memory']) == 41 and len(snapshot['budget']) == 20
    cases = {row['name']:row for row in snapshot['memory']}
    assert cases['select/0/False']['selected'] == []
    assert cases['select/-1/False']['selected'] == cases['select/None/False']['selected'][:-1]
    assert cases['select/list-fault']['error'] == 'initial list fault'
    assert cases['select/fallback-fault']['error'] == 'fallback fault'
    assert cases['consolidate/list-fault']['error'] == 'initial list fault'
    assert cases['select/tools-off']['calls']
    assert cases['prepare/disabled']['calls'] == []
    assert cases['prepare/tools-off']['calls'] == []
    assert cases['prepare/send-fault']['error'] == 'load sender fault'
    assert cases['consolidate/None']['calls'] == []
    assert cases['consolidate/2']['count'] == 2
    assert cases['consolidate/-1']['count'] == 2
    assert cases['consolidate/empty-zero']['calls']
    assert cases['consolidate/unhashable']['after'] == cases['consolidate/unhashable']['before']
    assert all(row['messages_unchanged'] for row in snapshot['memory'])
    assert all(len(row['foreign']) == 1 and row['foreign'][0]['owner'] == 'bob' for row in snapshot['memory'])
    budget = {row['name']:row for row in snapshot['budget']}
    assert budget['budget/write-fault']['error'] == 'second write fault'
    assert budget['budget/write-fault']['after'] == budget['budget/write-fault']['before']
    assert len(budget['budget/write-fault']['files']) == 1
    assert all('secret-value' not in body for row in snapshot['budget'] for body in row['files'].values())
    assert budget['budget/newest-only']['files'] == {}
    assert budget['budget/0/-1000']['count'] == 2
    assert snapshot['constants']['trajectory_schema'] == 'mini-loop.trajectory.v1'
