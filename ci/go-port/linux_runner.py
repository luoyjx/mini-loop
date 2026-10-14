#!/usr/bin/env python3
"""Bounded native Linux CI gate runner; the driver serializes all four modes.

--revision is an exact committed SHA, never a dirty checkout or inferred HEAD.
Source mode captures the six established platform-sensitive fixtures. Subsequent
full/vet/race modes require that completed Source run from the same revision.
Explicit absolute environment paths bind the repository, audit output, existing
Source assets and read-only module cache. The output's sealed linux-image.json
must name the approved derived image by ID. No local var evidence is modified.
Source uses /usr/bin/python3; native gates separately require CPython 3.11.3.
No downloads, image builds, package installs, retries, or assertion changes.
"""
from __future__ import annotations

import argparse
import ast
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import signal
import subprocess
import sys
import tarfile
import time
from datetime import datetime, timezone
import uuid
import zipfile

import host_pressure

def required_directory(name):
    raw = os.environ.get(name)
    if not raw:
        raise RuntimeError('Required absolute directory environment variable missing: ' + name)
    path = Path(raw)
    if not path.is_absolute() or not path.is_dir() or path.is_symlink() or path.resolve() != path:
        raise RuntimeError('Expected an existing canonical absolute directory: ' + name)
    return path


OUT = required_directory('MINILOOP_AUDIT_OUT')
ROOT = required_directory('MINILOOP_REPO_ROOT')
PRIOR = required_directory('MINILOOP_AUDIT_ASSETS')
IMAGE = None  # Bound from the sealed local image record before any launch.
IMAGE_RECORD = OUT / "linux-image.json"
MODCACHE = required_directory('MINILOOP_AUDIT_MODCACHE')
OWNER_LABEL = "io.mini-loop.cpython-linux.owner"
MEMORY_LIMIT = 3072 * 1024 * 1024
ALLOWED_PRESSURE = ('normal',)
PRESSURE_POLICY = host_pressure.POLICY
POLL_SECONDS = 2
PINNED_INPUTS = {
    "linux-source-plan.json": "f17af01da903e988e4bba8e6b999be7d9e87dc8459780a5d54c336e9eeac1a54",
    "linux-source-capture.py": "08c6ece6a7aa909f28d036bcc15c7697ddb9bb6ea807f7d0c5a975dd4110e632",
    "linux-wheel-manifest.json": "9c7205001057c49ffca8bcf4053afe01d6d4f9b970d217fefccc3926979d66e9",
}
GROUPS = ("benchmark", "httpapi", "httpapi-efficiency", "userresources", "workspace", "worktrees")

class StopRequested(BaseException):
    pass


def utc():
    return datetime.now(timezone.utc).isoformat()


def save(path, value):
    temporary = path.with_name(path.name + ".writing")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(path)


def append(path, value):
    with path.open("a") as handle:
        handle.write(json.dumps(value, ensure_ascii=False) + "\n")


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def bind_image():
    global IMAGE
    if IMAGE_RECORD.is_symlink() or not IMAGE_RECORD.is_file():
        raise RuntimeError('Sealed local linux-image.json is required before launch')
    payload = json.loads(IMAGE_RECORD.read_text())
    image = payload.get('image_id')
    if not isinstance(image, str) or not re.fullmatch(r'sha256:[0-9a-f]{64}', image):
        raise RuntimeError('linux-image.json image_id must be an exact sha256 image ID')
    IMAGE = image
    return {'path': str(IMAGE_RECORD), 'sha256': digest(IMAGE_RECORD),
            'bytes': IMAGE_RECORD.stat().st_size, 'image_id': image}


def pressure(destination, phase):
    """Record real Linux inputs, never translate them to macOS enum values."""
    row = {'utc': utc(), 'monotonic': time.monotonic(), 'stage': destination.name,
           'phase': phase, 'policy': PRESSURE_POLICY, 'status': 'unavailable'}
    try:
        row.update(host_pressure.observe())
    except host_pressure.PressureReadError as error:
        row.update(error.observation)
    except Exception as error:
        row['error'] = type(error).__name__ + ': ' + str(error)
    append(destination / 'host-pressure-readings.jsonl', row)
    append(OUT / 'host-pressure-readings.jsonl', row)
    if row['status'] == 'unavailable':
        raise StopRequested('host_memory_pressure_unavailable: ' + row['error'])
    return row['status']


def interrupted(signum, frame):
    raise StopRequested("signal " + str(signum))


def command(argv, destination, *, timeout=20, check=True):
    began = time.monotonic()
    row = {"utc": utc(), "monotonic": began, "argv": argv,
           "termination_operation": len(argv) > 1 and argv[0] == "docker" and argv[1] in ("stop", "kill", "rm")}
    try:
        result = subprocess.run(argv, text=True, capture_output=True, timeout=timeout)
        row.update(exit_code=result.returncode, stdout=result.stdout, stderr=result.stderr)
    except BaseException as error:
        row.update(error=type(error).__name__ + ": " + str(error))
        raise
    finally:
        row["seconds"] = time.monotonic() - began
        append(destination / "operations.jsonl", row)
    if check and result.returncode:
        raise RuntimeError("command failed: " + shlex.join(argv) + ": " + result.stderr[:1000])
    return result


def inspect_container(name, destination, token):
    result = command(["docker", "container", "inspect", name], destination, check=False)
    if result.returncode:
        if "No such container" in result.stderr or "No such object" in result.stderr:
            return None
        raise RuntimeError("container inspect unavailable: " + result.stderr[:500])
    data = json.loads(result.stdout)
    if len(data) != 1 or data[0]["Config"].get("Labels", {}).get(OWNER_LABEL) != token:
        raise RuntimeError("container ownership label does not match")
    if data[0]["Name"] != "/" + name:
        raise RuntimeError("container identity name does not match")
    return data[0]


def inspect_volume(name, destination, token):
    result = command(["docker", "volume", "inspect", name], destination, check=False)
    if result.returncode:
        if "no such volume" in result.stderr.lower():
            return None
        raise RuntimeError("volume inspect unavailable: " + result.stderr[:500])
    data = json.loads(result.stdout)
    if len(data) != 1 or data[0]["Name"] != name or (data[0].get("Labels") or {}).get(OWNER_LABEL) != token:
        raise RuntimeError("volume ownership does not match")
    return data[0]


def memory_bytes(display):
    value = display.split("/", 1)[0].strip()
    found = re.fullmatch(r"([0-9.]+)\s*(B|kB|KB|KiB|MB|MiB|GB|GiB|TB|TiB)", value)
    if found is None:
        raise ValueError("unrecognized Docker memory usage: " + display)
    scale = {"B": 1, "kB": 1000, "KB": 1000, "KiB": 1024,
             "MB": 1000**2, "MiB": 1024**2, "GB": 1000**3,
             "GiB": 1024**3, "TB": 1000**4, "TiB": 1024**4}
    return int(float(found[1]) * scale[found[2]])


def process_rss(destination, name, token, ident):
    """Observe owned PIDs only; an already exited container is not a failure."""
    sampled = command(['docker', 'top', ident, '-eo', 'pid,ppid,rss'], destination,
                      timeout=10, check=False)
    lines = [line.split() for line in sampled.stdout.splitlines() if line.strip()]
    if sampled.returncode or len(lines) < 2:
        latest = inspect_container(name, destination, token)
        if latest is None or latest['Id'] != ident or latest['State'].get('Running'):
            raise RuntimeError('Missing process RSS for running owned container')
        return {'status': 'container_exited_during_sample', 'pids': [],
                'exit_code': sampled.returncode}, latest
    if lines[0] != ['PID', 'PPID', 'RSS']:
        raise RuntimeError('Unexpected owned process RSS header')
    rows = []
    for fields in lines[1:]:
        if len(fields) != 3 or not all(value.isdecimal() for value in fields):
            raise RuntimeError('Malformed owned process RSS row')
        pid, parent, rss = map(int, fields)
        if pid == 0:
            raise RuntimeError('Invalid owned process identity')
        rows.append({'pid': pid, 'ppid': parent, 'rss_kib': rss})
    return {'status': 'sampled', 'pids': rows, 'rss_sum_kib': sum(row['rss_kib'] for row in rows),
            'scope': 'Owned container processes only; summed RSS may double-count shared pages and excludes Docker/host overhead.'}, None


def tree_hashes(root):
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink():
            raise RuntimeError('Unexpected symlink in owned immutable inputs: ' + str(path))
        if path.is_file():
            result[str(path.relative_to(root))] = digest(path)
    return result


def freeze(destination, revision):
    resolved = command(['git', '-C', str(ROOT), 'rev-parse', '--verify', revision + '^{commit}'], destination).stdout.strip()
    if resolved != revision:
        raise RuntimeError('Revision is not an exact existing commit SHA')
    archive = destination / 'input.tar'
    command(['git', '-C', str(ROOT), 'archive', '--format=tar', '--output=' + str(archive),
             revision, 'go', 'python'], destination, timeout=60)
    workspace = destination / 'workspace'
    workspace.mkdir()
    with tarfile.open(archive) as handle:
        members = handle.getmembers()
        seen = set()
        for member in members:
            path = PurePosixPath(member.name)
            if (path.is_absolute() or '..' in path.parts or not path.parts
                    or path.parts[0] not in ('go', 'python') or member.name in seen
                    or not (member.isfile() or member.isdir())):
                raise RuntimeError('Unsafe or duplicate archive member: ' + member.name)
            seen.add(member.name)
        handle.extractall(workspace, members=members)
    hashes = tree_hashes(workspace)
    save(destination / 'archive-manifest.json', hashes)
    return {'revision': revision, 'archive_sha256': digest(archive),
            'archive_bytes': archive.stat().st_size, 'archive_members': len(members),
            'manifest_sha256': digest(destination / 'archive-manifest.json'), 'files': len(hashes)}


def prepare_inputs(destination, revision, mode, capture_root):
    inputs = destination / 'inputs'
    inputs.mkdir()
    for name, expected in PINNED_INPUTS.items():
        path = PRIOR / name
        if path.is_symlink() or digest(path) != expected:
            raise RuntimeError('Prior pinned preparation input differs: ' + name)
    plan = json.loads((PRIOR / 'linux-source-plan.json').read_text())
    plan['baseline_commit'] = revision
    plan['baseline_note'] = 'Exact current committed archive; only the established six Source producers are invoked.'
    plan['status'] = 'prepared_for_current_revision'
    plan['derived_from_plan_sha256'] = PINNED_INPUTS['linux-source-plan.json']
    plan['current_outer_overrides'] = {
        'go_package_parallel': 1, 'test_parallel': 2, 'GOFLAGS': '-p=1',
        'memory_cgroup_bytes': MEMORY_LIMIT, 'memory_and_swap_bytes': MEMORY_LIMIT,
        'GOMEMLIMIT': '1GiB', 'allowed_host_pressure': list(ALLOWED_PRESSURE),
        'host_pressure_policy': PRESSURE_POLICY,
        'host_pressure_helper_sha256': digest(Path(host_pressure.__file__)),
        'note': 'Current remote p1/3GiB profile overrides the historical pinned plan resource values. Source worker clears application environment and executes no Go commands.'}
    plan['historical_image'] = dict(plan['image'])
    plan['image']['id'] = IMAGE
    plan['image']['native_parser'] = '/usr/local/bin/python3.11'
    plan['image']['native_parser_version'] = '3.11.3'
    plan['validation_done'] = False
    plan['dependencies']['python_requirements_sha256'] = digest(destination / 'workspace/python/requirements.txt')
    plan['source_hashes'] = {name: sha for name, sha in tree_hashes(destination / 'workspace').items()
                             if name.startswith('python/')}
    plan['capture']['script'] = 'inputs/capture.py'
    plan['capture']['groups_serial'] = list(GROUPS)
    plan['evidence'] = {'scope': 'Six same-platform Source fixtures, followed by separate complete Go gates; no full Python/Linux export claim.'}
    plan['container_contract']['serialization'] = 'Same OUT/.runner.lock as host source/Go gates.'
    for mount in plan['container_contract'].get('mounts', []):
        if mount.get('target') == '/workspace':
            mount['source'] = 'frozen git archive ' + revision + ' go+python'
    exporter = destination / 'workspace/python/tools/export_go_contracts.py'
    syntax = ast.parse(exporter.read_text())
    definitions = {node.name: node.lineno for node in syntax.body if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))}
    for group in GROUPS:
        row = plan['groups'][group]
        fixture = destination / 'workspace/go/testdata' / row['fixture']
        data = json.loads(fixture.read_text())
        row['expected_all_cases'] = [case['name'] for case in data['cases']]
        row['expected_top_level_keys'] = sorted(data)
        row['source']['line'] = definitions[row['source']['symbol']]
    shutil.copyfile(PRIOR / 'linux-source-capture.py', inputs / 'capture.py')
    shutil.copyfile(PRIOR / 'linux-wheel-manifest.json', inputs / 'wheel-manifest.json')
    save(inputs / 'plan.json', plan)
    proof = {'historical_preparation_pins': PINNED_INPUTS,
             'plan_sha256': digest(inputs / 'plan.json'), 'capture_script_sha256': digest(inputs / 'capture.py'),
             'current_case_counts': {group: len(plan['groups'][group]['expected_all_cases']) for group in GROUPS},
             'source_code_transforms': [], 'assertion_code_transforms': []}
    if mode == 'source':
        deps = destination / 'deps'
        deps.mkdir()
        wheels = json.loads((inputs / 'wheel-manifest.json').read_text())
        if len(wheels) != len(plan['dependencies']['capture_required']):
            raise RuntimeError('Wheel count differs from pinned dependencies')
        # Unpack existing, hash-verified wheels into this attempt's private tree.
        # No pip, import, installation, download or shared dependency mutation.
        for row in wheels:
            name = row['name']
            if Path(name).name != name or not name.endswith('.whl'):
                raise RuntimeError('Unsafe wheel name')
            wheel = PRIOR / 'linux-wheels' / name
            if wheel.is_symlink() or wheel.stat().st_size != row['bytes'] or digest(wheel) != row['sha256']:
                raise RuntimeError('Existing pinned wheel differs: ' + name)
            with zipfile.ZipFile(wheel) as archive:
                for item in archive.infolist():
                    relative = PurePosixPath(item.filename)
                    if (relative.is_absolute() or '..' in relative.parts or not relative.parts
                            or ((item.external_attr >> 16) & 0o170000) == 0o120000):
                        raise RuntimeError('Unsafe wheel entry: ' + item.filename)
                    target = deps.joinpath(*relative.parts)
                    if item.is_dir():
                        target.mkdir(parents=True, exist_ok=True)
                    else:
                        target.parent.mkdir(parents=True, exist_ok=True)
                        with target.open('xb') as output, archive.open(item) as source:
                            shutil.copyfileobj(source, output)
        save(destination / 'dependency-manifest.json', tree_hashes(deps))
        proof['dependency_manifest_sha256'] = digest(destination / 'dependency-manifest.json')
    else:
        proof['source_capture'] = validate_capture(capture_root, plan, revision, destination)
    proof['input_hashes'] = tree_hashes(inputs)
    save(destination / 'input-proof.json', proof)
    return proof


def validate_capture(capture_root, plan, revision, destination):
    if (capture_root is None or not capture_root.is_absolute() or not capture_root.is_dir()
            or capture_root.is_symlink() or capture_root.resolve() != capture_root
            or capture_root.name != 'source-capture' or capture_root.parent.name != 'logs'):
        raise RuntimeError('Use the canonical absolute completed stage/logs/source-capture path')
    outer_root = capture_root.parent.parent
    if outer_root.parent != OUT:
        raise RuntimeError('Source capture must come from this new runner output root')
    outer_path = outer_root / 'result.json'
    outer = json.loads(outer_path.read_text())
    if (outer.get('mode') != 'source' or outer.get('status') != 'source_captured'
            or outer.get('revision') != revision or outer.get('image') != IMAGE
            or outer.get('runner_exit_code') != 0 or outer.get('cleanup_errors')
            or outer.get('owned_resources_absent') is not True or outer.get('inputs_unchanged') is not True
            or outer.get('image_record', {}).get('sha256') != digest(IMAGE_RECORD)
            or outer.get('host_pressure_helper_sha256') != digest(Path(host_pressure.__file__))
            or outer.get('runner_sha256') != digest(Path(__file__))):
        raise RuntimeError('Source outer resource/freeze/identity/cleanup proof is incomplete')
    receipt_path = capture_root / 'receipt.json'
    receipt = json.loads(receipt_path.read_text())
    if (receipt.get('status') != 'captured_six_original_producers'
            or receipt.get('executed_full_export') is not False
            or [row['id'] for row in receipt.get('groups', [])] != list(GROUPS)
            or any(row.get('exit_code') != 0 or row.get('timed_out') for row in receipt.get('groups', []))):
        raise RuntimeError('Six actual Source groups are incomplete')
    if outer['log_hashes'].get('logs/source-capture/receipt.json') != digest(receipt_path):
        raise RuntimeError('Source inner receipt no longer matches outer proof')
    hashes = {}
    for name, row in receipt['files'].items():
        if Path(name).name != name:
            raise RuntimeError('Unsafe captured file name')
        path = capture_root / name
        if path.is_symlink() or path.stat().st_size != row['bytes'] or digest(path) != row['sha256']:
            raise RuntimeError('Source captured file differs: ' + name)
        hashes[name] = row['sha256']
    for row in plan['groups'].values():
        path = capture_root / row['fixture']
        if path.name not in hashes:
            raise RuntimeError('Source fixture hash missing')
        data = json.loads(path.read_text())
        if ([case['name'] for case in data['cases']] != row['expected_all_cases']
                or sorted(data) != row['expected_top_level_keys']):
            raise RuntimeError('Source fixture scope differs: ' + path.name)
    environment = json.loads((capture_root / 'environment.json').read_text())
    if (environment['source_hashes'] != plan['source_hashes']
            or environment['plan_sha256'] != digest(destination / 'inputs/plan.json')
            or environment['script_sha256'] != PINNED_INPUTS['linux-source-capture.py']):
        raise RuntimeError('Source capture plan or committed inputs differ')
    return {'capture_root': str(capture_root), 'receipt_sha256': digest(receipt_path),
            'outer_result_sha256': digest(outer_path), 'archive_sha256': outer['input']['archive_sha256'],
            'fixture_hashes': {plan['groups'][g]['fixture']: hashes[plan['groups'][g]['fixture']] for g in GROUPS},
            'environment': environment}


def worker_script(destination, mode):
    context_code = (
        "import hashlib,json,pathlib,platform,subprocess,sys; shell=pathlib.Path('/bin/sh').resolve(); "
        "print(json.dumps({'uname':list(platform.uname()),'python':platform.python_version(),"
        "'shell':{'path':str(shell),'sha256':hashlib.sha256(shell.read_bytes()).hexdigest()},"
        "'git':subprocess.check_output(['git','--version'],text=True).strip(),"
        "'tmp_filesystem':subprocess.check_output(['stat','-f','-c','%T','/tmp'],text=True).strip()})); "
        "sys.exit(0 if sys.implementation.name=='cpython' and platform.python_version()=='3.11.2' else 78)"
    )
    parser_context_code = (
        "import ast,base64,hashlib,json,os,resource,sys; "
        "version='.'.join(str(n) for n in sys.version_info[:3]); "
        "path=os.path.realpath(sys.executable); "
        "row={'implementation':sys.implementation.name,'version':version,"
        "'invoked_path':'/usr/local/bin/python3.11','executable':sys.executable,'resolved_path':path,"
        "'executable_sha256':hashlib.sha256(open(path,'rb').read()).hexdigest(),"
        "'required_imports':['ast','base64','json','resource'],'isolated':sys.flags.isolated,"
        "'no_site':sys.flags.no_site,'dont_write_bytecode':sys.dont_write_bytecode}; "
        "print(json.dumps(row,sort_keys=True)); "
        "sys.exit(0 if row['implementation']=='cpython' and version=='3.11.3' "
        "and row['isolated']==1 and row['no_site']==1 and row['dont_write_bytecode'] else 78)"
    )
    if mode == 'source':
        argv = ['nice', '-n', '10', '/usr/bin/python3', '-B', '/capture.py',
                '--source-root', '/workspace', '--plan', '/plan.json', '--output', '/out/source-capture']
    elif mode == 'vet':
        argv = ['nice', '-n', '10', 'go', 'vet', '-p=1', './...']
    else:
        argv = ['nice', '-n', '10', 'go', 'test', '-json', '-count=1', '-parallel=2', '-p=1', '-timeout=30m']
        if mode == 'race':
            argv.append('-race')
        argv.append('./...')
    lines = [
        '#!/bin/sh', 'set -u', 'umask 077', 'cd /workspace/go',
        'capture_cgroup() {',
        '  for field in memory.current memory.peak memory.events memory.max memory.swap.max cpu.max pids.max; do',
        '    if [ -r /sys/fs/cgroup/$field ]; then',
        '      cat /sys/fs/cgroup/$field > /out/$1.$field.writing',
        '      mv /out/$1.$field.writing /out/$1.$field',
        '    fi', '  done', '}',
        'mkdir -p /tmp/home/.cache/go-build /tmp/cache/gopath || exit 78',
        'capture_cgroup start',
        '(while :; do capture_cgroup sampled; sleep 2; done) &', 'sampler=$!',
        'finish() { kill "$sampler" 2>/dev/null || true; wait "$sampler" 2>/dev/null || true; capture_cgroup final; }',
        'trap finish EXIT',
        shlex.join(['/usr/bin/python3', '-B', '-c', context_code]) + ' > /out/platform-context.json 2> /out/platform-context.stderr.log',
        'platform_checked=$?', 'printf "%s\\n" "$platform_checked" > /out/platform-context.exit',
        'if [ "$platform_checked" -ne 0 ]; then exit 78; fi',
        'go version > /out/environment.log 2>&1',
        'go env GOOS GOARCH CGO_ENABLED CC GOCACHE GOMODCACHE GOPATH GOFLAGS >> /out/environment.log 2>&1',
    ]
    if mode != 'source':
        lines.extend([
                      shlex.join(['nice', '-n', '10', '/usr/local/bin/python3.11', '-I', '-S', '-B', '-c', parser_context_code])
                      + ' > /out/native-parser-context.json 2> /out/native-parser-context.stderr.log',
                      'parser_checked=$?', 'printf "%s\\n" "$parser_checked" > /out/native-parser-context.exit',
                      'if [ "$parser_checked" -ne 0 ]; then exit 78; fi',
                      'nice -n 10 go list -p=1 ./... > /out/packages.txt 2> /out/packages.stderr.log',
                      'listed=$?', 'printf "%s\\n" "$listed" > /out/packages.exit',
                      'if [ "$listed" -ne 0 ]; then exit "$listed"; fi'])
    lines.extend([
        'printf "%s\\n" ' + shlex.quote(mode) + ' > /out/current-stage',
        'date -u +%Y-%m-%dT%H:%M:%SZ > /out/stage.started',
        shlex.join(argv) + ' > /out/stdout.log 2> /out/stderr.log',
        'result=$?', 'printf "%s\\n" "$result" > /out/stage.exit',
        'date -u +%Y-%m-%dT%H:%M:%SZ > /out/stage.finished',
        'printf "complete\\n" > /out/current-stage', 'exit "$result"',
    ])
    script = destination / 'gates.sh'
    script.write_text('\n'.join(lines) + '\n')
    save(destination / 'commands.json', {'mode': mode, 'argv': argv, 'package_inventory': None if mode == 'source' else ['nice', '-n', '10', 'go', 'list', '-p=1', './...'],
                                         'source_platform_probe': ['/usr/bin/python3', '-B', '-c', context_code],
                                         'native_parser_probe': None if mode == 'source' else ['nice', '-n', '10', '/usr/local/bin/python3.11', '-I', '-S', '-B', '-c', parser_context_code],
                                         'coverage': False, 'selectors': [], 'automatic_retry': False})
    return script


def create_argv(destination, token, name, volume, script, mode, capture_root):
    env = {'HOME': '/tmp/home', 'GOCACHE': '/tmp/home/.cache/go-build', 'GOMODCACHE': '/gomod',
           'GOPATH': '/tmp/cache/gopath', 'GOTMPDIR': '/tmp', 'TMPDIR': '/tmp',
           'CGO_ENABLED': '1', 'GOMAXPROCS': '2', 'GOFLAGS': '-p=1', 'GOMEMLIMIT': '1GiB',
           'GOGC': '75', 'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOTOOLCHAIN': 'local',
           'LANG': 'C.UTF-8', 'PYTHONDONTWRITEBYTECODE': '1',
           'PATH': '/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'}
    if mode == 'source':
        env['PYTHONPATH'] = '/deps:/workspace/python'
    argv = ['docker', 'create', '--name', name, '--label', OWNER_LABEL + '=' + token,
            '--platform', 'linux/arm64', '--pull', 'never', '--init', '--read-only', '--network', 'none',
            '--cpus', '2', '--memory', str(MEMORY_LIMIT), '--memory-swap', str(MEMORY_LIMIT), '--pids-limit', '256']
    mounts = [(destination / 'workspace', '/workspace', True), (destination / 'logs', '/out', False),
              (MODCACHE, '/gomod', True), (script, '/gates.sh', True)]
    if mode == 'source':
        mounts.extend([(destination / 'deps', '/deps', True), (destination / 'inputs/capture.py', '/capture.py', True),
                       (destination / 'inputs/plan.json', '/plan.json', True)])
    else:
        plan = json.loads((destination / 'inputs/plan.json').read_text())
        for group in GROUPS:
            fixture = plan['groups'][group]['fixture']
            mounts.append((capture_root / fixture, '/workspace/go/testdata/' + fixture, True))
    for source, target, readonly in mounts:
        argv.extend(['--mount', 'type=bind,src=' + str(source) + ',dst=' + target + (',readonly' if readonly else '')])
    argv.extend(['--mount', 'type=volume,src=' + volume + ',dst=/tmp'])
    for key, value in env.items():
        argv.extend(['-e', key + '=' + value])
    argv.extend(['-w', '/workspace/go', IMAGE, 'sh', '/gates.sh'])
    return argv


def cleanup(destination, token, name, volume, result):
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, signal.SIG_IGN)
    errors = []
    container_absent = volume_absent = False
    try:
        state = inspect_container(name, destination, token)
        if state is not None:
            save(destination / 'container-before-cleanup.json', state)
            ident = state['Id']
            if state['State'].get('Running'):
                command(['docker', 'stop', '--time', '5', ident], destination, timeout=15, check=False)
            terminal = inspect_container(name, destination, token)
            if terminal is None or terminal['Id'] != ident:
                raise RuntimeError('Owned container identity unavailable before removal')
            if terminal['State'].get('Running'):
                command(['docker', 'kill', ident], destination, timeout=10, check=False)
                terminal = inspect_container(name, destination, token)
                if terminal is None or terminal['Id'] != ident:
                    raise RuntimeError('Owned container identity unavailable after kill')
            save(destination / 'container-terminal.json', terminal)
            result['container_state'] = terminal['State']
            result['container_limits'] = {key: terminal['HostConfig'].get(key) for key in
                                         ('Memory', 'MemorySwap', 'NanoCpus', 'PidsLimit', 'ReadonlyRootfs', 'NetworkMode')}
            command(['docker', 'rm', '--force', ident], destination, timeout=15)
            if terminal['State'].get('Running'):
                errors.append('Terminal worker exit not observed before exact owned removal')
        container_absent = inspect_container(name, destination, token) is None
        if not container_absent:
            errors.append('Owned container remains after cleanup')
    except Exception as error:
        errors.append('container cleanup: ' + str(error))
    try:
        if inspect_volume(volume, destination, token) is not None:
            command(['docker', 'volume', 'rm', volume], destination, timeout=15)
        volume_absent = inspect_volume(volume, destination, token) is None
        if not volume_absent:
            errors.append('Owned volume remains after cleanup')
    except Exception as error:
        errors.append('volume cleanup: ' + str(error))
    result.update(cleanup_errors=errors, owned_resources_absent=container_absent and volume_absent)


def collect_go(destination, mode):
    logs = destination / 'logs'
    packages = (logs / 'packages.txt').read_text().splitlines() if (logs / 'packages.txt').exists() else []
    output = {'expected_packages': packages, 'package_terminal': {}, 'test_counts': {'pass': 0, 'fail': 0, 'skip': 0},
              'subtest_counts': {'pass': 0, 'fail': 0, 'skip': 0}, 'non_json_stdout': [], 'duplicate_terminals': []}
    listing_exit = int((logs / 'packages.exit').read_text()) if (logs / 'packages.exit').exists() else None
    output['listing_exit_code'] = listing_exit
    if mode in ('full', 'race') and (logs / 'stdout.log').exists():
        with (logs / 'stdout.log').open() as handle:
            for number, line in enumerate(handle, 1):
                if not line.strip():
                    continue
                try:
                    row = json.loads(line)
                except ValueError:
                    output['non_json_stdout'].append({'line': number, 'text': line.rstrip('\n')})
                    continue
                if not isinstance(row, dict) or not isinstance(row.get('Action'), str):
                    output['non_json_stdout'].append({'line': number, 'text': line.rstrip('\n')})
                    continue
                action = row['Action']
                if action not in ('pass', 'fail', 'skip'):
                    continue
                test = row.get('Test')
                if test:
                    output['subtest_counts' if '/' in test else 'test_counts'][action] += 1
                else:
                    package = row.get('Package')
                    if package in output['package_terminal']:
                        output['duplicate_terminals'].append(package)
                    output['package_terminal'][package] = action
    output['complete_scope'] = (bool(packages) and len(packages) == len(set(packages)) and listing_exit == 0
                               and (mode == 'vet' or (set(output['package_terminal']) == set(packages)
                                    and all(value in ('pass', 'skip') for value in output['package_terminal'].values())
                                    and not output['non_json_stdout'] and not output['duplicate_terminals']
                                    and output['test_counts']['fail'] == 0 and output['subtest_counts']['fail'] == 0)))
    return output


def finalize(destination, result, mode, capture_root):
    logs = destination / 'logs'
    result['stage_exit_code'] = int((logs / 'stage.exit').read_text()) if (logs / 'stage.exit').exists() else None
    cgroup = {}
    for path in sorted(logs.glob('*.memory.events')):
        cgroup[path.name] = {key: int(value) for key, value in (line.split() for line in path.read_text().splitlines())}
    result['cgroup_memory_events'] = cgroup
    result['cgroup_peak_bytes'] = {p.name: int(p.read_text()) for p in logs.glob('*.memory.peak')}
    result['peak_measurement'] = 'Kernel memory.peak includes charged cgroup memory; Docker stats samples are working set, not process RSS. Final kernel peak is absent if the container cannot publish it.'
    source_context = logs / 'platform-context.json'
    source_exit = logs / 'platform-context.exit'
    result['source_python_verified'] = (source_context.is_file() and source_exit.is_file()
                                        and source_exit.read_text().strip() == '0'
                                        and json.loads(source_context.read_text()).get('python') == '3.11.2')
    if mode != 'source':
        result['go_results'] = collect_go(destination, mode)
        parser_context = logs / 'native-parser-context.json'
        parser_exit = logs / 'native-parser-context.exit'
        result['native_parser_probe_exit_code'] = int(parser_exit.read_text()) if parser_exit.is_file() else None
        if parser_context.is_file() and result['native_parser_probe_exit_code'] == 0:
            observed = json.loads(parser_context.read_text())
            result['native_parser_context'] = observed
            result['native_parser_verified'] = (
                observed.get('implementation') == 'cpython' and observed.get('version') == '3.11.3'
                and observed.get('invoked_path') == '/usr/local/bin/python3.11'
                and observed.get('required_imports') == ['ast', 'base64', 'json', 'resource']
                and observed.get('isolated') == 1 and observed.get('no_site') == 1
                and observed.get('dont_write_bytecode') is True
                and isinstance(observed.get('resolved_path'), str)
                and observed['resolved_path'].startswith('/usr/local/')
                and isinstance(observed.get('executable_sha256'), str)
                and re.fullmatch(r'[0-9a-f]{64}', observed['executable_sha256']) is not None)
        else:
            result['native_parser_verified'] = False
    if result['status'] == 'completed':
        state = result.get('container_state', {})
        if state.get('OOMKilled') or any(row.get('oom', 0) or row.get('oom_kill', 0) for row in cgroup.values()) or result['stage_exit_code'] == 137:
            result.update(status='resource_blocked', reason='container_oom_or_killed_worker', runner_exit_code=75)
        elif not (logs / 'final.memory.peak').exists() or not (logs / 'final.memory.events').exists():
            result.update(status='environment_blocked', reason='final_cgroup_measurement_missing', runner_exit_code=75)
        elif not result['source_python_verified']:
            result.update(status='environment_blocked', reason='required_source_cpython_3_11_2_probe_failed', runner_exit_code=75)
        elif mode == 'source':
            path = logs / 'source-capture/receipt.json'
            receipt = json.loads(path.read_text()) if path.exists() else {}
            result['source_receipt'] = receipt
            if any(row.get('timed_out') for row in receipt.get('groups', [])):
                result.update(status='resource_blocked', reason='source_recipe_wall_budget', runner_exit_code=75)
            elif (state.get('ExitCode') == 0 and result['stage_exit_code'] == 0
                  and receipt.get('status') == 'captured_six_original_producers'
                  and [row['id'] for row in receipt.get('groups', [])] == list(GROUPS)
                  and all(row.get('exit_code') == 0 for row in receipt.get('groups', []))):
                result.update(status='source_captured', runner_exit_code=0)
            else:
                result.update(status='failed', runner_exit_code=1)
        elif not result.get('native_parser_verified'):
            result.update(status='environment_blocked', reason='required_cpython_3_11_3_probe_failed', runner_exit_code=75)
        elif state.get('ExitCode') == 0 and result['stage_exit_code'] == 0 and result['go_results']['complete_scope']:
            result.update(status='passed', runner_exit_code=0)
        else:
            result.update(status='failed', runner_exit_code=1)
    if result.get('input'):
        before = json.loads((destination / 'archive-manifest.json').read_text())
        after = tree_hashes(destination / 'workspace')
        save(destination / 'archive-manifest.after.json', after)
        unchanged = (before == after and digest(destination / 'input.tar') == result['input']['archive_sha256'])
        if result.get('input_proof'):
            proof = result['input_proof']
            unchanged = unchanged and tree_hashes(destination / 'inputs') == proof['input_hashes']
            if mode == 'source':
                unchanged = unchanged and tree_hashes(destination / 'deps') == json.loads((destination / 'dependency-manifest.json').read_text())
            else:
                original = proof['source_capture']
                result['fixture_hashes_after'] = {name: digest(capture_root / name) for name in original['fixture_hashes']}
                unchanged = unchanged and result['fixture_hashes_after'] == original['fixture_hashes']
                unchanged = unchanged and digest(capture_root / 'receipt.json') == original['receipt_sha256']
                unchanged = unchanged and digest(capture_root.parent.parent / 'result.json') == original['outer_result_sha256']
                context_path = logs / 'platform-context.json'
                if context_path.exists():
                    actual = json.loads(context_path.read_text())
                    source = original['environment']
                    matches = (actual['shell'] == source['shell'] and actual['git'] == source['git']['stdout'].strip()
                               and actual['tmp_filesystem'] == source['tmp']['stat']['stdout'].strip()
                               and actual['uname'][0] == source['uname'][0] and actual['uname'][2:5] == source['uname'][2:5])
                    result['source_platform_context_matches'] = matches
                    if not matches:
                        result.update(status='environment_blocked', reason='source_go_platform_context_differs', runner_exit_code=75)
        unchanged = unchanged and digest(Path(__file__)) == result['runner_sha256']
        unchanged = unchanged and digest(Path(host_pressure.__file__)) == result['host_pressure_helper_sha256']
        unchanged = unchanged and (not IMAGE_RECORD.is_symlink() and IMAGE_RECORD.is_file()
                                   and digest(IMAGE_RECORD) == result['image_record']['sha256'])
        if result.get('worker_script_sha256'):
            unchanged = unchanged and digest(destination / 'gates.sh') == result['worker_script_sha256']
        result['inputs_unchanged'] = unchanged
        if not unchanged:
            result.update(status='environment_blocked', reason='frozen_inputs_changed', runner_exit_code=75)
    if result.get('cleanup_errors'):
        result.update(status='environment_blocked', reason='owned_cleanup_incomplete', runner_exit_code=75)
    result['log_hashes'] = {str(path.relative_to(destination)): digest(path) for path in sorted(logs.rglob('*')) if path.is_file()}
    for path in sorted((destination / 'inputs').glob('*')):
        if path.is_file():
            result['log_hashes'][str(path.relative_to(destination))] = digest(path)
    for path in sorted(destination.iterdir()):
        if path.is_file() and path.name not in ('result.json', 'input.tar'):
            result['log_hashes'][path.name] = digest(path)


def run(destination, revision, mode, capture_root, wall_seconds):
    token = uuid.uuid4().hex
    name = 'mini-loop-linux-full-' + mode + '-' + token
    volume = name + '-tmp'
    began = time.monotonic()
    attempted_resources = False
    result = {'status': 'prepared', 'mode': mode, 'revision': revision, 'image': IMAGE,
              'runner_sha256': digest(Path(__file__)), 'started_utc': utc(), 'runner_exit_code': 75,
              'host_pressure_helper_sha256': digest(Path(host_pressure.__file__)),
              'container_name': name, 'volume_name': volume, 'owner_token': token, 'launched': False,
              'allowed_host_pressure': list(ALLOWED_PRESSURE), 'wall_limit_seconds': wall_seconds,
              'host_pressure_policy': PRESSURE_POLICY,
              'host_pressure_readings': str(destination / 'host-pressure-readings.jsonl'),
              'host_pressure_aggregate': str(OUT / 'host-pressure-readings.jsonl'),
              'configured_paths': {'repository': str(ROOT), 'audit_output': str(OUT),
                                   'assets': str(PRIOR), 'module_cache': str(MODCACHE)},
              'acceptance_scope': 'Six same-platform Source fixture captures' if mode == 'source' else 'Complete go ' + mode + ' ./... with exactly six same-platform fixture overlays',
              'profile': {'cpu_cgroup_cores': 2, 'memory_cgroup_bytes': MEMORY_LIMIT, 'memory_and_swap_bytes': MEMORY_LIMIT,
                          'gomaxprocs': 2, 'gogc': 75, 'gomemlimit': '1GiB soft target', 'go_package_parallel': 1,
                          'test_parallel': 2, 'nice': 10, 'pids': 256, 'preflight_samples': 5, 'automatic_retries': False},
              'full_port_acceptance': False}
    save(destination / 'result.json', result)
    try:
        result['image_record'] = bind_image()
        result['image'] = IMAGE
        save(destination / 'image-record.json', json.loads(IMAGE_RECORD.read_text()))
        for sample in range(5):
            value = pressure(destination, 'preflight')
            append(destination / 'memory.jsonl', {'utc': utc(), 'phase': 'preflight', 'host_pressure': value})
            if value not in ALLOWED_PRESSURE:
                raise StopRequested('host_memory_pressure_before_launch')
            if sample != 4:
                time.sleep(1)
        if not MODCACHE.is_dir():
            raise RuntimeError('Read-only module cache missing')
        image = json.loads(command(['docker', 'image', 'inspect', IMAGE], destination).stdout)[0]
        if image['Id'] != IMAGE or image['Architecture'] != 'arm64' or image['Os'] != 'linux':
            raise RuntimeError('Pinned image identity/platform mismatch')
        save(destination / 'image.json', {key: image.get(key) for key in ('Id', 'Architecture', 'Os', 'RepoDigests')})
        result['input'] = freeze(destination, revision)
        result['input_proof'] = prepare_inputs(destination, revision, mode, capture_root)
        if mode != 'source' and result['input_proof']['source_capture']['archive_sha256'] != result['input']['archive_sha256']:
            raise RuntimeError('Source and native archives differ')
        (destination / 'logs').mkdir()
        script = worker_script(destination, mode)
        result['worker_script_sha256'] = digest(script)
        attempted_resources = True
        command(['docker', 'volume', 'create', '--label', OWNER_LABEL + '=' + token, volume], destination)
        if inspect_volume(volume, destination, token) is None:
            raise RuntimeError('Owned volume was not created')
        argv = create_argv(destination, token, name, volume, script, mode, capture_root)
        save(destination / 'container-command.json', argv)
        command(argv, destination)
        created = inspect_container(name, destination, token)
        if created is None:
            raise RuntimeError('Owned container was not created')
        save(destination / 'container-created.json', created)
        config = created['HostConfig']
        if (config.get('Memory') != MEMORY_LIMIT or config.get('MemorySwap') != MEMORY_LIMIT
                or config.get('NanoCpus') != 2000000000 or config.get('PidsLimit') != 256
                or not config.get('ReadonlyRootfs') or config.get('NetworkMode') != 'none'):
            raise RuntimeError('Container hard limits differ from requested profile')
        ident = created['Id']
        value = pressure(destination, 'immediately_before_start')
        append(destination / 'memory.jsonl', {'utc': utc(), 'phase': 'immediately_before_start', 'host_pressure': value})
        if value not in ALLOWED_PRESSURE:
            raise StopRequested('host_memory_pressure_before_start')
        if time.monotonic() - began >= wall_seconds:
            raise StopRequested('owned_preparation_wall_budget')
        command(['docker', 'start', ident], destination)
        result.update(status='running', launched=True, container_started_utc=utc())
        save(destination / 'result.json', result)
        while True:
            value = pressure(destination, 'running')
            row = {'utc': utc(), 'monotonic': time.monotonic(), 'elapsed_seconds': time.monotonic() - began, 'host_pressure': value}
            if value not in ALLOWED_PRESSURE:
                append(destination / 'memory.jsonl', row)
                raise StopRequested('host_memory_pressure')
            if row['elapsed_seconds'] >= wall_seconds:
                append(destination / 'memory.jsonl', row)
                raise StopRequested('owned_validation_wall_budget')
            state = inspect_container(name, destination, token)
            if state is None or state['Id'] != ident:
                raise RuntimeError('Owned container disappeared or changed identity')
            row['container_state'] = state['State']
            if state['State'].get('Running'):
                stats = command(['docker', 'stats', '--no-stream', '--format', '{{json .}}', ident], destination, timeout=10, check=False)
                lines = [line for line in stats.stdout.splitlines() if line.strip()]
                if stats.returncode == 0 and len(lines) == 1:
                    row['docker_stats'] = json.loads(lines[0])
                    row['memory_working_set_bytes'] = memory_bytes(row['docker_stats']['MemUsage'])
                    if row['memory_working_set_bytes'] > MEMORY_LIMIT:
                        append(destination / 'memory.jsonl', row)
                        raise StopRequested('container_memory_budget')
                else:
                    latest = inspect_container(name, destination, token)
                    if latest is None or latest['Id'] != ident or latest['State'].get('Running'):
                        raise RuntimeError('Missing stats for running owned container')
                    state = latest
                    row['container_state'] = latest['State']
            if state['State'].get('Running'):
                row['owned_process_rss'], exited = process_rss(destination, name, token, ident)
                if exited is not None:
                    state = exited
                    row['container_state'] = exited['State']
            append(destination / 'memory.jsonl', row)
            if not state['State'].get('Running'):
                save(destination / 'container-completed.json', state)
                break
            time.sleep(POLL_SECONDS)
        result['status'] = 'completed'
    except StopRequested as error:
        result.update(status='resource_blocked', reason=str(error), runner_exit_code=75)
    except Exception as error:
        result.update(status='environment_blocked', reason=type(error).__name__ + ': ' + str(error), runner_exit_code=75)
    finally:
        for sig in (signal.SIGINT, signal.SIGTERM):
            signal.signal(sig, signal.SIG_IGN)
        if attempted_resources:
            cleanup(destination, token, name, volume, result)
        else:
            # No Docker resource creation was attempted; no removal claim is needed.
            result.update(cleanup_errors=[], owned_resources_absent=True)
        try:
            finalize(destination, result, mode, capture_root)
        except Exception as error:
            result.update(status='environment_blocked', reason='receipt_failure: ' + str(error), runner_exit_code=75)
        result.update(finished_utc=utc(), seconds=time.monotonic() - began)
        save(destination / 'result.json', result)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--revision', required=True, help='Exact committed 40-hex SHA for all four modes')
    parser.add_argument('--mode', choices=('source', 'full', 'vet', 'race'), required=True)
    parser.add_argument('--stage', required=True, help='Fresh safe linux-* stage name; existing evidence is never overwritten')
    parser.add_argument('--capture-root', type=Path, help='Required for full/vet/race: absolute current source-stage/logs/source-capture')
    parser.add_argument('--wall-seconds', type=int, default=3600)
    args = parser.parse_args()
    if not re.fullmatch(r'[0-9a-f]{40}', args.revision):
        parser.error('--revision must be a lower-case exact 40-hex commit')
    if not re.fullmatch(r'linux-[a-z0-9][a-z0-9-]{0,79}', args.stage) or not 1800 <= args.wall_seconds <= 3600:
        parser.error('Use a safe linux-* name and a wall budget between 1800 and 3600 seconds')
    if (args.mode != 'source') != (args.capture_root is not None):
        parser.error('--capture-root is required only for full/vet/race')
    with (OUT / '.runner.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print(json.dumps({'status': 'resource_blocked', 'reason': 'another_stage_runner_holds_lock', 'runner_exit_code': 75}))
            return 75
        destination = OUT / args.stage
        destination.mkdir()
        for sig in (signal.SIGINT, signal.SIGTERM):
            signal.signal(sig, interrupted)
        result = run(destination, args.revision, args.mode, args.capture_root, args.wall_seconds)
        print(json.dumps({'status': result['status'], 'runner_exit_code': result['runner_exit_code'],
                          'result': str(destination / 'result.json')}))
        return result['runner_exit_code']


if __name__ == '__main__':
    raise SystemExit(main())
