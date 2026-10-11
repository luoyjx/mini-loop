from tools.provider_background_options_contracts import provider_background_options_contracts


def test_background_options_acceptance_precedes_post_spawn_faults(tmp_path):
    observed = provider_background_options_contracts(tmp_path)
    rows = {row['name']: row for row in observed['cases']}
    assert len(rows) == 48
    for row in rows.values():
        assert row['output'].startswith('Started background task bg_0001: ')
        assert row['trace'] == ['before', 'guard', 'after', 'result']
        assert row['outcomes'] == [{'output': row['output'], 'denied': False, 'failed': False}]
        assert row['starts_at_return'] == 0
        assert row['starts_at_settlement'] == 1
        assert row['counter'] == 1 and row['live_at_settlement'] == 0
        assert row['notifications'] == [{'bg_id': 'bg_0001', 'status': row['status'], 'result': row['result']}]
        assert row['check'] == f"[{row['status']}] {row['result']}"
    for name, source_kind in [('numeric-text', 'str'), ('text', 'str'), ('array', 'list'), ('object', 'dict')]:
        row = rows['timeout-' + name]
        assert row['status'] == 'error'
        assert row['result'] == f"Error: '<=' not supported between instances of '{source_kind}' and 'int'"
        assert row['kills_at_settlement'] == 0
        assert row['warnings'] == [{'class': 'RuntimeWarning',
            'message': "coroutine 'BackgroundManager._exec.<locals>._capture' was never awaited"}]
    huge = rows['timeout-huge-integer']
    assert huge['result'] == 'Error: int too large to convert to float'
    assert huge['kills_at_settlement'] == 0
    assert len(huge['warnings']) == 1
    for name, label in [('fraction', '0.005'), ('negative', '-1'), ('negative-fraction', '-0.5'),
                        ('negative-infinity', '-inf'), ('nan', 'nan'), ('true-expired', 'True')]:
        row = rows['timeout-' + name]
        assert row['status'] == 'error'
        assert row['result'] == f'Error: Timeout ({label}s)'
        assert row['kills_at_settlement'] == 1
        assert row['warnings'] == []
    for name in ['missing', 'null', 'false', 'true', 'zero', 'float-zero', 'negative-zero',
                 'integer', 'empty-text', 'empty-array', 'empty-object', 'infinity']:
        row = rows['timeout-' + name]
        assert row['status'] == 'completed' and row['result'] == 'options-done'
        assert row['kills_at_settlement'] == 0 and row['warnings'] == []
    for name, row in rows.items():
        if name.startswith('prefix-'):
            assert row['status'] == 'completed' and row['result'] == 'options-done'
            assert row['kills_at_settlement'] == 0 and row['warnings'] == []
            assert row['default_candidate'] == ('background_run', 'printf', 'options-done')
            expected = ('background_run', 'printf', 'options-done') if name == 'prefix-valid' else None
            assert row['proposed_candidate'] == expected


def test_nan_timeout_observer_watchdog_changes_source_timer_heap(tmp_path):
    import asyncio
    from unittest.mock import patch
    from mini_loop.background import BackgroundManager

    async def observe(guard):
        root = tmp_path / ('guarded' if guard else 'unguarded')
        root.mkdir()
        manager = BackgroundManager(root)
        processes = []
        original = asyncio.create_subprocess_exec
        async def spawn(*args, **kwargs):
            process = await original(*args, **kwargs)
            processes.append(process)
            return process
        with patch.object(asyncio, 'create_subprocess_exec', spawn):
            try:
                manager.run('sleep 0.03; printf nan-done', float('nan'))
                handle = manager._tasks['bg_0001']['handle']
                if guard:
                    await asyncio.wait_for(asyncio.shield(handle), timeout=3)
                else:
                    await handle
                task = manager._tasks['bg_0001']
                return task['status'], task['result']
            finally:
                for process in processes:
                    await process.communicate()
                await manager.close()
    assert asyncio.run(observe(False)) == ('error', 'Error: Timeout (nans)')
    assert asyncio.run(observe(True)) == ('completed', 'nan-done')
