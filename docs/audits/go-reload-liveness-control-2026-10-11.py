"""Diagnostic: measure a control root on matched native subscriptions.

PYTHONPATH=python .venv/bin/python FILE PROBE_BINARY OUTPUT_JSON
Each case uses one shared root and operation. This never rewrites acceptance fixtures.
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

import uvicorn
import uvicorn.supervisors.watchfilesreload as reload_module
import watchfiles
import watchfiles.main as watch_main
from tools.watchfiles_contracts import _config


def source_worker():
    observer = None
    root = None
    opened = closed = 0
    original_notify, original_watch = watch_main.RustNotify, reload_module.watch

    class CountedNotify:
        def __init__(self, *args):
            nonlocal opened
            self.value = original_notify(*args)
            if args[2] or 'FsEventWatcher' not in repr(self.value):
                raise RuntimeError('actual native FsEventWatcher required')
            opened += 1

        def __enter__(self):
            return self.value.__enter__()

        def __exit__(self, *args):
            nonlocal closed
            result = self.value.__exit__(*args)
            closed += 1
            return result

    def drain():
        while observer.should_restart() is not None:
            pass

    def wait(stage):
        deadline = 5 if stage == 'probe' else 3
        expired = threading.Event()
        paths = set()

        def timeout():
            expired.set()
            observer.should_exit.set()

        timer = threading.Timer(deadline, timeout)
        timer.start()
        try:
            while True:
                changed = observer.should_restart()
                if changed is not None:
                    paths.update(str(p) for p in changed)
                if stage == 'probe' and changed is not None and root / 'pair_ready.py' in changed:
                    drain()
                    return None
                if expired.is_set():
                    if stage == 'next':
                        return sorted(paths)
                    raise TimeoutError('context deadline exceeded')
        except StopIteration:
            if expired.is_set() and stage == 'next':
                return sorted(paths)
            if expired.is_set():
                raise TimeoutError('context deadline exceeded') from None
            raise
        finally:
            timer.cancel()
            timer.join()

    with patch.dict(os.environ, {'WATCHFILES_FORCE_POLLING': 'false',
                                 'WATCHFILES_DEBUG': '', 'WATCHFILES_POLL_DELAY_MS': ''}), \
            patch.object(watch_main, 'RustNotify', CountedNotify), \
            patch.object(reload_module, 'watch', lambda *args, **kwargs:
                         original_watch(*args, **kwargs, rust_timeout=100)):
        try:
            for line in sys.stdin:
                cmd = json.loads(line)
                stage, start = cmd['kind'], time.monotonic()
                row = {'stage': stage, 'pid': os.getpid(), 'paths': None, 'error': ''}
                try:
                    if stage == 'start':
                        if observer is not None:
                            raise RuntimeError('subscription already open')
                        root = Path(cmd['root'])
                        config = _config(root)
                        config.reload_dirs = [root, Path(cmd['control_root'])]
                        observer = reload_module.WatchFilesReload(config, lambda sockets: None, [])
                    elif stage == 'exit':
                        if observer is not None:
                            raise RuntimeError('exit with open subscription')
                    elif observer is None:
                        raise RuntimeError('no open subscription')
                    elif stage == 'drain':
                        drain()
                    elif stage in {'probe', 'next'}:
                        row['paths'] = wait(stage)
                    elif stage == 'close':
                        observer.should_exit.set()
                        observer.watcher.close()
                        observer = None
                    else:
                        raise ValueError(stage)
                except TimeoutError:
                    row['error'] = 'context deadline exceeded'
                except Exception as error:
                    row['error'] = repr(error)
                row.update(opened=opened, closed=closed, elapsed_ms=(time.monotonic()-start)*1000)
                print(json.dumps(row), flush=True)
                if stage == 'exit':
                    return
        finally:
            if observer is not None:
                observer.should_exit.set()
                observer.watcher.close()


class Peer:
    def __init__(self, argv):
        self.process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, text=True)
        self.output = queue.Queue()

        def read():
            for line in self.process.stdout:
                try:
                    self.output.put(json.loads(line))
                except Exception as error:
                    self.output.put({'error': repr(error)})

        self.reader = threading.Thread(target=read)
        self.reader.start()

    def send(self, kind, root='', control_root=''):
        self.process.stdin.write(json.dumps({'kind': kind, 'root': str(root), 'control_root': str(control_root)})+'\n')
        self.process.stdin.flush()

    def receive(self, kind):
        row = self.output.get(timeout=10)
        if row.get('stage') != kind:
            raise RuntimeError(f'unexpected response: {row}')
        return row

    def finish(self):
        self.send('exit')
        row = self.receive('exit')
        self.process.stdin.close()
        self.process.wait(timeout=10)
        self.reader.join(5)
        if self.reader.is_alive():
            raise RuntimeError('output reader did not join')
        row.update(exit_code=self.process.returncode, stderr=self.process.stderr.read())
        if row['error'] or row['exit_code']:
            raise RuntimeError(f'unclean child exit: {row}')
        return row

    def abort(self):
        if self.process.poll() is None:
            self.process.stdin.close()
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                self.process.wait(timeout=10)
        self.reader.join(5)


def new_pair(binary):
    return {'go': Peer([binary]), 'source': Peer([sys.executable, __file__, '--source-worker'])}


def paired(recipe, peers, profile, ordinal):
    with tempfile.TemporaryDirectory(prefix='mini-loop-history-') as scratch:
        parent = Path(scratch).resolve()
        root = parent / 'business'
        control = parent / 'control'
        root.mkdir()
        control.mkdir()
        source = root / recipe['path']
        if recipe.get('directory_alias'):
            (root / recipe['directory_target']).mkdir()
            (root / recipe['directory_alias']).symlink_to(recipe['directory_target'])
        source.parent.mkdir(parents=True, exist_ok=True)
        if recipe.get('symlink_target'):
            (root / recipe['symlink_target']).write_text('before\n')
            source.symlink_to(recipe['symlink_target'])
        if recipe['initial']:
            source.write_text('before\n')
            os.utime(source, (100, 100))
        row = {'name': recipe['name'], 'profile': profile, 'ordinal': ordinal,
               'root': str(root), 'control_root': str(control), 'recipe': recipe}

        def exchange(stage):
            for peer in peers.values():
                peer.send(stage, root, control)
            return {side: peer.receive(stage) for side, peer in peers.items()}

        try:
            row['start'] = exchange('start')
            row['drain'] = exchange('drain')
            if any(result['error'] for stage in ['start', 'drain'] for result in row[stage].values()):
                raise RuntimeError('construction or pre-probe drain failed')
            for peer in peers.values():
                peer.send('probe')
            (root / 'pair_ready.py').write_text('# shared readiness\n')
            row['probe'] = {side: peer.receive('probe') for side, peer in peers.items()}
            if any(result['error'] for result in row['probe'].values()):
                row['outcome'] = 'readiness-failure'
                return row
            row['operation_epoch_ns'] = time.time_ns()
            operation = recipe['operation']
            if operation in {'write', 'add', 'unchanged-write'}:
                source.write_text('before\n' if operation == 'unchanged-write' else 'after\n')
                os.utime(source, (recipe['mtime'], recipe['mtime']))
            elif operation == 'remove':
                source.unlink()
            elif operation == 'rename':
                source.rename(root / recipe['destination'])
            elif operation == 'touch':
                os.utime(source, (recipe['mtime'], recipe['mtime']))
            elif operation == 'mkdir':
                source.mkdir()
            elif operation == 'transient':
                source.write_text('temporary\n')
                source.unlink()
            else:
                raise ValueError(operation)
            def sentinel():
                row['control_epoch_ns'] = time.time_ns()
                (control / 'stream_alive.py').write_text('# independent stream sentinel\n')
            timer = threading.Timer(1, sentinel)
            timer.start()
            try:
                row['next'] = exchange('next')
            finally:
                timer.cancel()
                timer.join()
            row['business_paths'] = {
                side: sorted(str(Path(path).relative_to(root)) for path in result['paths'] or []
                             if Path(path).is_relative_to(root))
                for side, result in row['next'].items()}
            row['control_seen'] = {side: str(control / 'stream_alive.py') in (result['paths'] or [])
                                   for side, result in row['next'].items()}
            row['matches_fixture'] = {side: row['business_paths'][side] == recipe['changed'] and not result['error']
                                      for side, result in row['next'].items()}
            row['outcome'] = 'operation-observed'
            return row
        finally:
            row['close'] = exchange('close')
            if any(result['error'] or result['opened'] != result['closed']
                   for result in row['close'].values()):
                raise RuntimeError(f'close counters disagree: {row["close"]}')


def controller(binary, output):
    fixture = json.loads(Path('go/testdata/python-watchfiles.json').read_text())
    report = {'watchfiles_version': watchfiles.__version__, 'uvicorn_version': uvicorn.__version__,
              'design': '16 recipes x fresh/reused; same native subscription watches business + independent control root; collect full 3s operation window with control mutation at 1s',
              'deadlines': {'operation_seconds': 3, 'probe_seconds': 5, 'rust_timeout_ms': 100},
              'source_timeout': 'full-window next timer sets should_exit at 3s; closes before next case',
              'cases': [], 'exits': []}
    reused = new_pair(binary)
    try:
        for ordinal, recipe in enumerate(fixture['native_events']):
            profiles = ['fresh', 'reused'] if ordinal % 2 == 0 else ['reused', 'fresh']
            for profile in profiles:
                peers = reused if profile == 'reused' else new_pair(binary)
                try:
                    row = paired(recipe, peers, profile, ordinal)
                    report['cases'].append(row)
                    print(profile, row['name'], row.get('matches_fixture', row['outcome']), flush=True)
                    if profile == 'fresh':
                        report['exits'].append({side: peer.finish() for side, peer in peers.items()})
                    Path(output).write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
                finally:
                    if profile == 'fresh':
                        for peer in peers.values():
                            peer.abort()
        report['exits'].append({side: peer.finish() for side, peer in reused.items()})
        Path(output).write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    finally:
        for peer in reused.values():
            peer.abort()


if __name__ == '__main__':
    if sys.argv[1:] == ['--source-worker']:
        source_worker()
    else:
        controller(*sys.argv[1:])
