#!/usr/bin/env python3
"""Preserve audit bytes in Actions logs using only the Python standard library.

The log envelope is data, never a command. Cache, dependencies, workspace trees,
and Git metadata are excluded. Extraction verifies the whole compressed digest
and rejects links, traversal, duplicates and oversized archives before writing.
The large runner Node binary at build-context/node is omitted. Its source,
copy and derived-image bytes are checked by the original CI; replay binds the
retained probe/digest evidence and cannot re-read that omitted executable.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import tarfile
import tempfile
import uuid

STAGES = ('linux-ci-source', 'linux-ci-full', 'linux-ci-vet', 'linux-ci-race')
ROOT_FILES = frozenset(('bootstrap.json', 'linux-image.json', 'host-pressure-readings.jsonl',
                        'ci-context.json', 'verification.json', 'driver-result.json',
                        'driver-memory.jsonl', 'derived-image.id'))
SMALL_TREES = ('logs', 'bootstrap-logs', 'download-metadata', 'module-inputs', 'build-context')
ASSET_FILES = ('Dockerfile', 'linux-source-plan.json', 'linux-source-capture.py',
               'linux-wheel-manifest.json', 'remote-go-image-resolution.json')
MAX_COMPRESSED = 64 * 1024 * 1024
MAX_UNCOMPRESSED = 512 * 1024 * 1024
MAX_FILE = 128 * 1024 * 1024
MAX_MEMBERS = 20000
CHUNK = 12 * 1024


def members(root: Path):
    """Select only root receipts and owned stage evidence, never cache trees."""
    selected = []
    for name in sorted(ROOT_FILES):
        path = root / name
        if path.exists() or path.is_symlink():
            selected.append(path)
    for name in ASSET_FILES:
        path = root / 'assets' / name
        if path.exists() or path.is_symlink():
            selected.append(path)
    for name in SMALL_TREES:
        tree = root / name
        if tree.is_symlink():
            raise ValueError('bootstrap evidence directory is a link')
        if tree.exists():
            for path in tree.rglob('*'):
                if path.relative_to(root).as_posix() == 'build-context/node':
                    continue
                if path.is_symlink():
                    raise ValueError('bootstrap evidence contains a link')
                if path.is_file():
                    selected.append(path)
    for name in STAGES:
        stage = root / name
        if not stage.exists():
            continue
        if stage.is_symlink() or not stage.is_dir():
            raise ValueError('stage must be an owned directory')
        selected.extend(path for path in stage.iterdir() if path.is_file() or path.is_symlink())
        for directory in ('logs', 'inputs'):
            tree = stage / directory
            if tree.is_symlink():
                raise ValueError('evidence directory is a link')
            if tree.exists():
                for path in tree.rglob('*'):
                    if path.is_symlink():
                        raise ValueError('evidence contains a link')
                    if path.is_file():
                        selected.append(path)
    total = 0
    names = set()
    for path in sorted(selected):
        metadata = path.lstat()
        if not stat.S_ISREG(metadata.st_mode) or not path.resolve().is_relative_to(root.resolve()):
            raise ValueError('evidence must be an owned regular file')
        name = path.relative_to(root).as_posix()
        if name in names or metadata.st_size > MAX_FILE:
            raise ValueError('duplicate or oversized evidence')
        names.add(name)
        total += metadata.st_size
        if total > MAX_UNCOMPRESSED or len(names) > MAX_MEMBERS:
            raise ValueError('evidence budget exceeded')
        yield name, path


def emit(root: Path):
    archive = root / 'evidence.tar.gz'
    with archive.open('xb') as raw:
        with tarfile.open(fileobj=raw, mode='w:gz', compresslevel=6) as stream:
            for name, path in members(root):
                stream.add(path, arcname=name, recursive=False)
    size = archive.stat().st_size
    if size > MAX_COMPRESSED:
        raise ValueError('compressed evidence budget exceeded')
    with archive.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    ident = uuid.uuid4().hex
    count = (size + CHUNK - 1) // CHUNK
    header = {'id': ident, 'sha256': digest, 'bytes': size, 'chunks': count}
    print('MINILOOP_EVIDENCE_BEGIN ' + json.dumps(header, sort_keys=True), flush=True)
    with archive.open('rb') as stream:
        for index in range(count):
            print('MINILOOP_EVIDENCE_CHUNK ' + ident + ' ' + str(index) + ' '
                  + base64.b64encode(stream.read(CHUNK)).decode('ascii'), flush=True)
    print('MINILOOP_EVIDENCE_END ' + ident, flush=True)
    return header


def extract(log: Path, destination: Path, requested_id: str | None = None):
    if destination.exists() or destination.is_symlink():
        raise ValueError('refuse overwriting an evidence directory')
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='mini-loop-evidence-', dir=destination.parent) as temporary:
        archive_path = Path(temporary) / 'evidence.tar.gz'
        header = decode(log, archive_path, requested_id)
        staging = Path(temporary) / 'files'
        staging.mkdir()
        total, names = 0, set()
        with tarfile.open(archive_path, mode='r:gz') as archive:
            for member in archive:
                name = PurePosixPath(member.name)
                if (name.is_absolute() or '..' in name.parts or not name.parts
                        or member.name in names or not member.isfile() or member.size > MAX_FILE):
                    raise ValueError('unsafe evidence archive member')
                names.add(member.name)
                total += member.size
                if len(names) > MAX_MEMBERS or total > MAX_UNCOMPRESSED:
                    raise ValueError('uncompressed evidence budget exceeded')
                target = staging.joinpath(*name.parts)
                target.parent.mkdir(parents=True, exist_ok=True)
                with archive.extractfile(member) as source, target.open('xb') as output:
                    shutil.copyfileobj(source, output)
        with (staging / 'transport.json').open('x') as stream:
            json.dump(header, stream, indent=2)
            stream.write('\n')
        staging.rename(destination)
    return header


def decode(log: Path, archive: Path, requested_id: str | None):
    """Verify sequential log chunks directly into a private temporary file."""
    header = None
    count, size = 0, 0
    digest = hashlib.sha256()
    ended = False
    with log.open(errors='strict') as stream, archive.open('xb') as output:
        for line in stream:
            start = line.find('MINILOOP_EVIDENCE_')
            if start < 0:
                continue
            value = line[start:].strip()
            if value.startswith('MINILOOP_EVIDENCE_BEGIN '):
                candidate = json.loads(value.split(' ', 1)[1])
                if requested_id is not None and candidate['id'] != requested_id:
                    continue
                if header is not None:
                    raise ValueError('multiple evidence envelopes; choose one explicit id')
                if (not re.fullmatch(r'[0-9a-f]{32}', candidate['id'])
                        or not re.fullmatch(r'[0-9a-f]{64}', candidate['sha256'])
                        or not 0 < candidate['bytes'] <= MAX_COMPRESSED
                        or candidate['chunks'] != (candidate['bytes'] + CHUNK - 1) // CHUNK):
                    raise ValueError('invalid evidence header')
                header = candidate
            elif header is not None and value.startswith('MINILOOP_EVIDENCE_CHUNK '):
                _, ident, index, encoded = value.split(' ', 3)
                if ident != header['id']:
                    continue
                number = int(index)
                if ended or number != count or not 0 <= number < header['chunks']:
                    raise ValueError('duplicate or out-of-order chunk')
                chunk = base64.b64decode(encoded, validate=True)
                expected = CHUNK if number + 1 < header['chunks'] else header['bytes'] - number * CHUNK
                if len(chunk) != expected:
                    raise ValueError('invalid chunk size')
                output.write(chunk)
                digest.update(chunk)
                size += len(chunk)
                count += 1
            elif header is not None and value == 'MINILOOP_EVIDENCE_END ' + header['id']:
                if ended:
                    raise ValueError('duplicate evidence end')
                ended = True
    if header is None or not ended or count != header['chunks']:
        raise ValueError('incomplete evidence envelope')
    if size != header['bytes'] or digest.hexdigest() != header['sha256']:
        raise ValueError('evidence digest mismatch')
    return header


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--log', required=True, type=Path)
    parser.add_argument('--out', required=True, type=Path)
    parser.add_argument('--id')
    args = parser.parse_args()
    print(json.dumps(extract(args.log, args.out, args.id), sort_keys=True))


if __name__ == '__main__':
    main()
