"""Actual Source batch-local ledger and exclusive/parallel failure effects."""
import json
from pathlib import Path

from tools.batch_step_boundary_contracts import batch_step_boundary_contracts


def test_source_mixed_batch_step_admission():
    snapshot = batch_step_boundary_contracts()
    root = Path(__file__).resolve().parents[2]
    assert snapshot == json.loads((root/'go/testdata/python-batch-step-boundary.json').read_text())
    assert len(snapshot['cases']) == 32
    cases = {row['name']:row for row in snapshot['cases']}
    for row in snapshot['cases']:
        if row['bad'] == -1:
            assert row['error'] is None and row['output'] == 'done'
            assert row['requests'] == row['model_starts'] == 2
            assert len(row['steps']) == 3 and row['messages'] == 4
            assert row['result_ids'] == ['id-0','id-1','id-2']
        else:
            assert row['error'] == 'UnicodeEncodeError'
            assert row['requests'] == row['model_starts'] == 1
            assert row['steps'] == row['result_ids'] == []
            assert row['messages'] == 2
    for capacity in [1,2]:
        assert cases[f'exclusive/bad-0/capacity-{capacity}']['effects'] == [0]
        assert cases[f'exclusive/bad-1/capacity-{capacity}']['effects'] == [0,1]
        for bad in [0,1,2]:
            assert cases[f'parallel/bad-{bad}/capacity-{capacity}']['effects'] == [0,1,2]
        assert cases[f'parallel-barrier/bad-0/capacity-{capacity}']['effects'] == [0]
