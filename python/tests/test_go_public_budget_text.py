"""Pin actual Source preflight, callback order and partial effects."""
import json
from pathlib import Path

from tools.public_budget_text_contracts import public_budget_text_contracts


def test_source_public_budget_text_boundary():
    snapshot = public_budget_text_contracts()
    root = Path(__file__).resolve().parents[2]
    assert snapshot == json.loads((root/'go/testdata/python-public-budget-text.json').read_text())
    assert len(snapshot['cases']) == 25
    cases = {row['name']:row for row in snapshot['cases']}
    for name in ['text/high/0','text/high/200000','text/low/0',
                 'text/literal-pair/0','text/later/0','mask-can-not-rescue-raw']:
        row = cases[name]
        assert row['error']['kind'] == 'UnicodeEncodeError'
        assert row['trace'] == [] and row['files'] == {}
        assert not row['output_directory'] and not row['message_replaced']
    assert cases['plain/spill']['trace'] == ['clock','mask']
    assert cases['plain/spill']['directory_at_clock'] == [True]
    assert cases['directory-blocked']['error']['kind'] == 'FileExistsError'
    assert cases['directory-blocked']['trace'] == []
    for name in ['mask-fault-first','mask-invalid-first']:
        assert cases[name]['output_directory']
        assert cases[name]['files'] == {}
    for name in ['mask-fault-second','mask-invalid-second']:
        assert len(cases[name]['files']) == 1
        assert not cases[name]['message_replaced']
    assert list(cases['id/high-surrogate']['files']) == ['start_end-123456.txt']
    assert list(cases['id/surrogate-pair-literal']['files']) == ['start__end-123456.txt']
    assert list(cases['id/character-cap']['files']) == ['A'*98+'__-123456.txt']
    assert cases['older-invalid-ignored']['error'] is None
    assert all(row['before_unchanged_on_error'] for row in snapshot['cases'])


def test_actual_source_ast_loop_error_event_and_transcript_stage():
    from tools.ast_text_contracts import ast_text_contracts

    snapshot = ast_text_contracts()
    root = Path(__file__).resolve().parents[2]
    assert snapshot == json.loads((root/'go/testdata/python-ast-text.json').read_text())
    assert len(snapshot['invocations']) == 8
    for row in snapshot['invocations']:
        loop = row['agent']
        assert not loop['budget_directory']
        assert 'tool_result' in loop['events']
        if row['utf8']:
            assert loop['error'] == '' and loop['output'] == 'done'
            assert loop['provider_calls'] == loop['model_starts'] == 2
            assert loop['transcript_messages'] == 4
            assert loop['result_text_hex'] == row['text_hex']
        else:
            assert loop['error'] == 'UnicodeEncodeError'
            assert loop['provider_calls'] == loop['model_starts'] == 1
            assert loop['transcript_messages'] == 2 and loop['result_text_hex'] is None
            assert loop['error_frames'][-1]['function'] == 'step_hash'
