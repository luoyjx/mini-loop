"""Diagnostic-only logging around the existing matched-history experiment.

PYTHONPATH=python .venv/bin/python FILE INSTRUMENTED_PROBE OUTPUT_JSON TRACE_DIR
Go C entry logging is supplied by a build overlay, never a runtime source edit.
"""
import hashlib
import importlib.util
import itertools
import json
import os
import subprocess
import sys
import time
from pathlib import Path
from unittest.mock import patch

import watchfiles.main as watch_main


def load_history():
    path = Path(__file__).with_name('go-reload-history-control-2026-10-11.py')
    spec = importlib.util.spec_from_file_location('reload_history_diagnostic', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    # The existing coordinator launches this file for the Source worker.
    module.__file__ = __file__
    return module


def source_worker(history):
    original = watch_main.RustNotify

    class TracedNotify:
        def __init__(self, *args):
            self.value = original(*args)
            self.roots = args[0]

        def __repr__(self):
            return repr(self.value)

        def __enter__(self):
            self.value.__enter__()
            return self

        def __exit__(self, *args):
            return self.value.__exit__(*args)

        def watch(self, *args):
            began = time.time_ns()
            value = self.value.watch(*args)
            record = {'begin_ns': began, 'return_ns': time.time_ns(), 'pid': os.getpid(),
                      'roots': self.roots, 'backend': repr(self.value),
                      'result': value if isinstance(value, str) else 'changes',
                      'changes': [] if isinstance(value, str) else sorted([int(kind), path] for kind, path in value)}
            # stderr is an owned file in the coordinator, avoiding a full pipe.
            print('MINILOOP_RUST '+json.dumps(record), file=sys.stderr, flush=True)
            return value

    with patch.object(watch_main, 'RustNotify', TracedNotify):
        history.source_worker()


def controller(history, binary, output, trace_dir):
    trace_root = Path(trace_dir)
    trace_root.mkdir(mode=0o700)
    original_popen = subprocess.Popen
    original_peer = history.Peer
    sequence = itertools.count()

    def logged_popen(argv, **kwargs):
        side = 'source' if argv[-1] == '--source-worker' else 'go'
        path = trace_root / f'{next(sequence):02d}-{side}.log'
        with path.open('w') as stream:
            process = original_popen(argv, **{**kwargs, 'stderr': stream})
        process.stderr = path.open('r')
        return process

    class TracedPeer(original_peer):
        def finish(self):
            row = super().finish()
            self.process.stderr.close()
            return row

        def abort(self):
            try:
                super().abort()
            finally:
                self.process.stderr.close()

    with patch.object(history, 'Peer', TracedPeer), patch.object(subprocess, 'Popen', logged_popen):
        history.controller(binary, output)
    report = json.loads(Path(output).read_text())
    report['capture'] = {'go_boundary': 'C callback entry before Go forwarding',
                         'source_boundary': 'RustNotify.watch return before WatchFiles/Uvicorn filtering',
                         'clock': 'CLOCK_REALTIME / time.time_ns; event IDs are not timestamps',
                         'stderr_sha256': {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                                           for p in sorted(trace_root.iterdir())},
                         'limits': ['Logging may affect scheduling; C entry and Rust binding return are different boundaries.',
                                    'No instrumentation inside Source notify/FSEvents callback.',
                                    'Interleaved single cohort does not isolate shared volume or daemon state.',
                                    'The unchanged history controller retains its Source timer/Go context cancellation distinction.']}
    Path(output).write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')


if __name__ == '__main__':
    history = load_history()
    if sys.argv[1:] == ['--source-worker']:
        source_worker(history)
    else:
        controller(history, *sys.argv[1:])
