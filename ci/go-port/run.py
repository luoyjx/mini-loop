#!/usr/bin/env python3
"""Run the fixed Go-port acceptance stages on an isolated GitHub arm64 VM."""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import re
import signal
import subprocess
import sys
import time
import traceback

import transport
from host_pressure import PressureReadError, observe


def save(path, value):
    with path.open('x') as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write('\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo-root', required=True, type=Path)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--revision', required=True)
    args = parser.parse_args()
    if (platform.system() != 'Linux' or platform.machine() not in ('aarch64', 'arm64')
            or os.environ.get('GITHUB_ACTIONS') != 'true' or os.geteuid() != 0):
        parser.error('Heavy execution requires the dedicated root-owned GitHub Linux arm64 job')
    repository = args.repo_root.resolve(strict=True)
    out = args.out.resolve()
    if out.exists() or not re.fullmatch(r'[0-9a-f]{40}', args.revision):
        parser.error('Use a fresh output directory and an exact revision')
    head = subprocess.check_output(['git', '-C', str(repository), 'rev-parse', 'HEAD'], text=True).strip()
    if head != args.revision or os.environ.get('GITHUB_SHA') != head:
        parser.error('Checkout, requested revision and workflow SHA must match')
    context = {key: os.environ.get(key) for key in ('GITHUB_REPOSITORY', 'GITHUB_SHA', 'GITHUB_RUN_ID',
                                                  'GITHUB_RUN_ATTEMPT', 'GITHUB_REF',
                                                  'GITHUB_WORKFLOW_SHA', 'GITHUB_WORKFLOW_REF')}
    context.update(schema_version=1, started_utc=datetime.now(timezone.utc).isoformat(),
                   checkout=str(repository), output=str(out), platform=platform.platform(),
                   runtime_code_baseline='606630f5c894b03cfccaafa1d2158e3bb07863cf',
                   scope='fixed current-runtime Linux Source/full/vet/race; not full port acceptance')
    environment = {key: os.environ[key] for key in ('PATH', 'GITHUB_ACTIONS', 'GITHUB_REPOSITORY', 'GITHUB_SHA',
                                                   'GITHUB_RUN_ID', 'GITHUB_RUN_ATTEMPT', 'GITHUB_REF',
                                                   'GITHUB_WORKFLOW_SHA', 'GITHUB_WORKFLOW_REF') if key in os.environ}
    environment.update(MINILOOP_AUDIT_OUT=str(out), MINILOOP_REPO_ROOT=str(repository),
                       MINILOOP_AUDIT_ASSETS=str(out / 'assets'), MINILOOP_AUDIT_MODCACHE=str(out / 'modules'),
                       PYTHONUNBUFFERED='1', PYTHONDONTWRITEBYTECODE='1')
    status, commands = 'environment_blocked', []
    deadline = time.monotonic() + 75 * 60
    child = None
    stop_reason = None
    pressure_rows = []
    forwarded = False

    def request_stop(signum, _frame):
        nonlocal stop_reason
        stop_reason = 'driver_signal_' + str(signum)

    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, request_stop)

    def sample_pressure(phase):
        nonlocal stop_reason
        try:
            row = observe()
            if row['status'] != 'normal':
                stop_reason = 'driver_host_memory_warning'
        except PressureReadError as error:
            row = dict(error.observation)
            stop_reason = 'driver_host_memory_observation_unavailable'
        except Exception as error:
            row = {'status': 'unavailable', 'error': type(error).__name__ + ': ' + str(error)}
            stop_reason = 'driver_host_memory_observation_unavailable'
        row.update(utc=datetime.now(timezone.utc).isoformat(), phase=phase)
        pressure_rows.append(row)
        if out.is_dir():
            with (out / 'driver-memory.jsonl').open('a') as stream:
                for pending in pressure_rows:
                    stream.write(json.dumps(pending, sort_keys=True) + '\n')
            pressure_rows.clear()

    def stop_child():
        nonlocal forwarded
        if child is not None and child.poll() is None and not forwarded:
            child.send_signal(signal.SIGTERM)
            forwarded = True

    def execute(command):
        nonlocal child, forwarded, stop_reason
        phase = Path(command[1]).name
        if '--mode' in command:
            phase += ':' + command[command.index('--mode') + 1]
        sample_pressure('before_' + phase)
        if time.monotonic() >= deadline and not stop_reason:
            stop_reason = 'driver_wall_budget'
        if stop_reason:
            raise RuntimeError(stop_reason)
        print(json.dumps({'event': 'command_start', 'argv': command}), flush=True)
        forwarded = False
        record = {'argv': command, 'started_utc': datetime.now(timezone.utc).isoformat()}
        commands.append(record)
        child = subprocess.Popen(command, cwd=repository, env=environment, start_new_session=True)
        record['owned_child_pid'] = child.pid
        cleanup_deadline = None
        while child.poll() is None:
            sample_pressure(phase)
            if time.monotonic() >= deadline and not stop_reason:
                stop_reason = 'driver_wall_budget'
            if stop_reason:
                stop_child()
                if cleanup_deadline is None:
                    cleanup_deadline = time.monotonic() + 180
                if time.monotonic() >= cleanup_deadline and child.poll() is None:
                    child.kill()  # Only this unreaped direct child, never a guessed PID or unrelated process.
                    child.wait()
                    record.update(returncode=child.returncode, stop_signal_forwarded=forwarded,
                                  cleanup_budget_exceeded=True)
                    raise RuntimeError('Owned child exceeded cleanup budget: ' + stop_reason)
            try:
                child.wait(timeout=1)
            except subprocess.TimeoutExpired:
                pass
        code = child.wait()
        record.update(returncode=code, stop_signal_forwarded=forwarded,
                      finished_utc=datetime.now(timezone.utc).isoformat())
        child = None
        print(json.dumps({'event': 'command_end', 'returncode': code}), flush=True)
        if code or stop_reason:
            raise RuntimeError('Required command did not complete: exit ' + str(code) + '; ' + str(stop_reason))

    try:
        execute([sys.executable, '-m', 'unittest', 'discover', '-s', 'ci/go-port', '-p', 'test_*.py', '-q'])
        execute([sys.executable, 'ci/go-port/bootstrap.py', '--repo-root', str(repository), '--out', str(out)])
        save(out / 'ci-context.json', context)
        for mode in ('source', 'full', 'vet', 'race'):
            command = [sys.executable, 'ci/go-port/linux_runner.py', '--revision', head,
                       '--mode', mode, '--stage', 'linux-ci-' + mode]
            if mode != 'source':
                command += ['--capture-root', str(out / 'linux-ci-source/logs/source-capture')]
            execute(command)
        execute([sys.executable, 'ci/go-port/verify.py', '--repo-root', str(repository),
                 '--out', str(out), '--revision', head])
        status = 'linux_gates_verified'
    except Exception as error:
        status = 'resource_blocked' if stop_reason else 'gate_or_environment_failed'
        print(json.dumps({'event': 'driver_error', 'error': type(error).__name__ + ': ' + str(error)}), flush=True)
        traceback.print_exc()
    finally:
        stop_child()
        if child is not None:
            try:
                child.wait(timeout=180)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait()
        out.mkdir(parents=True, exist_ok=True)
        if not (out / 'ci-context.json').exists():
            save(out / 'ci-context.json', context)
        if pressure_rows:
            with (out / 'driver-memory.jsonl').open('a') as stream:
                for row in pressure_rows:
                    stream.write(json.dumps(row, sort_keys=True) + '\n')
        save(out / 'driver-result.json', {'status': status, 'commands': commands,
             'finished_utc': datetime.now(timezone.utc).isoformat(), 'workflow_commit': head,
             'stop_reason': stop_reason, 'full_port_acceptance': False, 'required_row_delta_applied': False})
        transport.emit(out)
    return 0 if status == 'linux_gates_verified' else 1


if __name__ == '__main__':
    raise SystemExit(main())
