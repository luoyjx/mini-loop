"""Source profiles retain explicit-empty caller provenance through real turns."""
import json
from pathlib import Path
from tools.public_run_context_contracts import public_run_context_contracts


def test_public_run_context_source_profiles():
    actual = public_run_context_contracts()
    expected = json.loads((Path(__file__).resolve().parents[2]/'go/testdata/python-public-run-context.json').read_text())
    assert actual == expected
    assert len(actual['cases']) == 70
    for row in actual['cases']:
        assert row['tool_contexts'] == [row['snapshot']]
        assert row['output'] == 'done' and row['requests'] == 2 and row['messages'] == 4
        assert row['result'] == row['snapshot']['authority']
        assert row['fresh_message'] == (row['mode'] != 'identity')
        assert len(row['events']) == 2
        if row['mode'].startswith('derive-'):
            assert row['snapshot']['authority'] == 'peer_agent'
            assert row['snapshot']['approved_capabilities'] == []
    rows = {row['name']: row for row in actual['cases']}
    assert rows['human-empty-provenance/identity']['snapshot']['channel'] == ''
    assert rows['human-empty-provenance/identity']['snapshot']['stamped_by'] == ''
    assert rows['peer-empty-parent/identity']['events'][0]['parent_present']
    assert rows['peer-empty-parent/identity']['events'][0]['parent_message_id'] == ''
    peer_grants = rows['peer-default/new-grants']
    assert peer_grants['allows']['workflow.launch']
    assert peer_grants['snapshot']['authority'] == 'peer_agent'
