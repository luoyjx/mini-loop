"""Diagnostic only: paired actual Uvicorn/WatchFiles and Go on one owned root.

Usage: PYTHONPATH=python .venv/bin/python FILE PROBE_BINARY OUTPUT_JSON
This is not an acceptance fixture or retry-until-green harness.
"""
import json
import os
import queue
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path
from unittest.mock import patch

import watchfiles
import uvicorn
import watchfiles.main as watch_main
import uvicorn.supervisors.watchfilesreload as reload_module
from tools.watchfiles_contracts import _config


def paired(recipe, binary):
    with tempfile.TemporaryDirectory(prefix='mini-loop-paired-') as scratch:
        root = Path(scratch).resolve()
        source = root / recipe['path']
        source.parent.mkdir(parents=True, exist_ok=True)
        if recipe.get('symlink_target'):
            (root / recipe['symlink_target']).write_text('before\n')
            source.symlink_to(recipe['symlink_target'])
        if recipe['initial']:
            source.write_text('before\n')
            os.utime(source, (100, 100))
        process = subprocess.Popen([binary, str(root)], stdin=subprocess.PIPE,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        go_output = queue.Queue()

        def read_go():
            for line in process.stdout:
                go_output.put(json.loads(line))

        reader = threading.Thread(target=read_go, name='paired-go-output')
        reader.start()
        commands, source_output = queue.Queue(), queue.Queue()
        backend = []
        original_notify, original_watch = watch_main.RustNotify, reload_module.watch

        class ActualNotify:
            def __init__(self, *args):
                self.value = original_notify(*args)
                representation = repr(self.value)
                if args[2] or 'FsEventWatcher' not in representation:
                    raise RuntimeError('paired Source must use actual FsEventWatcher')
                backend.append(representation)

            def __enter__(self):
                return self.value.__enter__()

            def __exit__(self, *args):
                return self.value.__exit__(*args)

        def bounded_watch(*args, **kwargs):
            return original_watch(*args, **kwargs, rust_timeout=100)

        def go_command(kind):
            process.stdin.write(json.dumps({'kind': kind}) + '\n')
            process.stdin.flush()

        records = {}
        observer = None
        thread = None
        try:
            records['go_construct'] = go_output.get(timeout=8)
            if records['go_construct']['error']:
                raise RuntimeError(records['go_construct']['error'])
            with patch.dict(os.environ, {'WATCHFILES_FORCE_POLLING':'false',
                                         'WATCHFILES_DEBUG':'', 'WATCHFILES_POLL_DELAY_MS':''}), \
                    patch.object(watch_main, 'RustNotify', ActualNotify), \
                    patch.object(reload_module, 'watch', bounded_watch):
                observer = reload_module.WatchFilesReload(_config(root), lambda sockets: None, [])

                def source_worker():
                    try:
                        while True:
                            stage = commands.get()
                            if stage == 'close':
                                return
                            start = time.monotonic()
                            changed = None
                            if stage == 'drain':
                                while observer.should_restart() is not None:
                                    pass
                            elif stage == 'probe':
                                end = start + 5
                                while time.monotonic() < end:
                                    changed = observer.should_restart()
                                    if changed is not None and root / 'pair_ready.py' in changed:
                                        break
                                else:
                                    raise TimeoutError('Source readiness probe')
                                while observer.should_restart() is not None:
                                    pass
                            elif stage == 'next':
                                while not observer.should_exit.is_set():
                                    changed = observer.should_restart()
                                    if changed is not None:
                                        break
                            source_output.put({'stage':stage, 'paths':None if changed is None else sorted(str(p.relative_to(root)) for p in changed),
                                               'error':'', 'elapsed_ms':(time.monotonic()-start)*1000})
                    except BaseException as error:
                        source_output.put({'stage':'error','paths':None,'error':repr(error)})

                thread = threading.Thread(target=source_worker, name='paired-source-watchfiles')
                thread.start()
                go_command('drain'); commands.put('drain')
                records['go_drain'] = go_output.get(timeout=8)
                records['source_drain'] = source_output.get(timeout=8)
                if records['go_drain']['error'] or records['source_drain']['error']:
                    raise RuntimeError('paired pre-probe drain failed')
                go_command('probe'); commands.put('probe')
                (root / 'pair_ready.py').write_text('# paired readiness\n')
                records['go_probe'] = go_output.get(timeout=8)
                records['source_probe'] = source_output.get(timeout=8)
                if records['go_probe']['error'] or records['source_probe']['error']:
                    raise RuntimeError('paired readiness failed')
                records['operation_epoch_ns'] = time.time_ns()
                operation = recipe['operation']
                if operation in {'write','add','unchanged-write'}:
                    source.write_text('before\n' if operation=='unchanged-write' else 'after\n')
                    os.utime(source,(recipe['mtime'],recipe['mtime']))
                elif operation == 'remove': source.unlink()
                elif operation == 'rename': source.rename(root / recipe['destination'])
                elif operation == 'touch': os.utime(source,(recipe['mtime'],recipe['mtime']))
                elif operation == 'mkdir': source.mkdir()
                elif operation == 'transient':
                    source.write_text('temporary\n'); source.unlink()
                else: raise ValueError(operation)
                go_command('next'); commands.put('next')
                started = time.monotonic()
                try:
                    records['source_next'] = source_output.get(timeout=3)
                except queue.Empty:
                    records['source_next'] = {'stage':'next','paths':None,'error':'operation deadline exceeded',
                                              'elapsed_ms':(time.monotonic()-started)*1000}
                records['go_next'] = go_output.get(timeout=5)
                records['same_result'] = (records['go_next']['paths'] == records['source_next']['paths']
                                          and bool(records['go_next']['error']) == bool(records['source_next']['error']))
                records['matches_fixture'] = {side: records[side+'_next']['paths'] == recipe['changed']
                                              and not records[side+'_next']['error'] for side in ['go','source']}
                observer.should_exit.set(); commands.put('close'); thread.join(5)
                if thread.is_alive(): raise RuntimeError('Source worker did not join')
                observer.watcher.close()
                records['source_closed'] = True
        except Exception as error:
            records['control_error'] = repr(error)
        finally:
            if observer is not None:
                observer.should_exit.set(); commands.put('close')
                if thread is not None:
                    thread.join(5)
                    if thread.is_alive(): raise RuntimeError('Source cleanup did not join')
                observer.watcher.close()
            if process.poll() is None:
                go_command('close')
                records['go_close'] = go_output.get(timeout=8)
                process.stdin.close()
                process.wait(timeout=8)
            reader.join(5)
            if reader.is_alive(): raise RuntimeError('Go output reader did not join')
            records['go_exit_code'] = process.returncode
            records['go_stderr'] = process.stderr.read()
            records['source_backend'] = backend
            records['name'] = recipe['name']
            records['recipe'] = recipe
        return records


if __name__ == '__main__':
    binary, output = sys.argv[1:]
    fixture = json.loads(Path('go/testdata/python-watchfiles.json').read_text())
    result = {'watchfiles_version':watchfiles.__version__, 'uvicorn_version':uvicorn.__version__,
              'cases':[],'scope':'same root, same Python filename, one shared operation; 3s operation / 5s probe deadlines'}
    for recipe in fixture['native_events']:
        row = paired(recipe, binary)
        result['cases'].append(row)
        Path(output).write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
        print(row['name'], row.get('matches_fixture',{}), row.get('control_error',''), flush=True)
