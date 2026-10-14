#!/usr/bin/env python3
"""Small real-code checks for the CI evidence log boundary; no CI/container work."""
from __future__ import annotations

import base64
from contextlib import redirect_stdout
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import random
import tarfile
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location("go_port_evidence_transport", Path(__file__).with_name("transport.py"))
transport = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(transport)


class EvidenceTransportTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="mini-loop-transport-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def put(self, root: Path, name: str, content: bytes) -> None:
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)

    def emitted_log(self, root: Path) -> tuple[dict, list[str]]:
        output = io.StringIO()
        with redirect_stdout(output):
            header = transport.emit(root)
        return header, output.getvalue().splitlines()

    def log_file(self, lines: list[str], name: str = "job.log") -> Path:
        path = self.root / name
        # GitHub's downloaded job log prefixes every stdout line. Other job
        # output is intentionally present on both sides of the envelope.
        framed = ["2026-10-15T00:00:00.0000000Z setup: preparing public inputs"]
        framed += ["2026-10-15T00:00:01.1234567Z " + line for line in lines]
        framed += ["2026-10-15T00:00:02.0000000Z job: completed"]
        path.write_text("\n".join(framed) + "\n", encoding="utf-8")
        return path

    def valid_evidence(self) -> tuple[Path, dict, list[str]]:
        source = self.root / "evidence"
        source.mkdir()
        self.put(source, "bootstrap.json", b'{"status":"prepared"}\n')
        # Incompressible, deterministic bytes force several real transport
        # chunks; this also catches accidental UTF-8/newline transformations.
        self.put(source, "linux-ci-full/logs/stdout.log", random.Random(17).randbytes(65536))
        header, lines = self.emitted_log(source)
        self.assertGreater(header["chunks"], 2)
        return source, header, lines

    def assert_rejected_without_destination(self, lines: list[str], name: str = "bad") -> None:
        destination = self.root / name
        log = self.log_file(lines, name + ".log")
        with self.assertRaises(ValueError):
            transport.extract(log, destination)
        self.assertFalse(os.path.lexists(destination), "rejected evidence published a destination")

    def test_timestamped_job_log_preserves_complete_bytes_and_cache_isolation(self):
        source = self.root / "evidence"
        source.mkdir()
        expected = {
            "bootstrap.json": b'{"status":"prepared"}\n',
            "linux-image.json": b'{"image_id":"fixture-only"}\r\n',
            "linux-ci-source/resources.json": b'{"status":"source_captured"}\n',
            "linux-ci-full/logs/stdout.log": b"\x00\xff\r\n" + random.Random(7).randbytes(65536),
            "linux-ci-race/inputs/plan.json": b'{"scope":"complete"}\n',
        }
        for name, content in expected.items():
            self.put(source, name, content)
        for name in (
            "modules/private-cache", "wheels/private.whl", "assets/linux-wheels/private.whl",
            "workspace/python/private.py", ".git/config", "not-evidence-private.txt",
            "linux-ci-full/modules/private-cache", "linux-ci-full/wheels/private.whl",
            "linux-ci-full/workspace/go/private.go", "linux-ci-full/.git/config",
        ):
            self.put(source, name, b"THIS_PRIVATE_SENTINEL_MUST_NOT_BE_TRANSPORTED")
        header, lines = self.emitted_log(source)
        destination = self.root / "decoded"
        observed = transport.extract(self.log_file(lines), destination, header["id"])
        self.assertEqual(observed, header)
        for name, content in expected.items():
            self.assertEqual((destination / name).read_bytes(), content, name)
        forbidden = {"modules", "wheels", "linux-wheels", "workspace", ".git"}
        for path in destination.rglob("*"):
            self.assertFalse(forbidden.intersection(path.relative_to(destination).parts), str(path))
            if path.is_file():
                self.assertNotIn(b"THIS_PRIVATE_SENTINEL_MUST_NOT_BE_TRANSPORTED", path.read_bytes())
        self.assertFalse((destination / "not-evidence-private.txt").exists())
        self.assertEqual(json.loads((destination / "transport.json").read_text()), header)

    def test_missing_chunk_is_rejected(self):
        _, _, lines = self.valid_evidence()
        chunks = [index for index, line in enumerate(lines) if line.startswith("MINILOOP_EVIDENCE_CHUNK ")]
        del lines[chunks[1]]
        self.assert_rejected_without_destination(lines)

    def test_duplicate_chunk_is_rejected(self):
        _, _, lines = self.valid_evidence()
        index = next(index for index, line in enumerate(lines) if line.startswith("MINILOOP_EVIDENCE_CHUNK "))
        lines.insert(index + 1, lines[index])
        self.assert_rejected_without_destination(lines)

    def test_changed_payload_with_original_digest_is_rejected(self):
        _, _, lines = self.valid_evidence()
        index = next(index for index, line in enumerate(lines) if line.startswith("MINILOOP_EVIDENCE_CHUNK "))
        marker, ident, ordinal, payload = lines[index].split(" ", 3)
        changed = bytearray(base64.b64decode(payload))
        changed[-1] ^= 1
        lines[index] = " ".join((marker, ident, ordinal, base64.b64encode(changed).decode("ascii")))
        self.assert_rejected_without_destination(lines)

    def test_changed_header_digest_is_rejected(self):
        _, _, lines = self.valid_evidence()
        header = json.loads(lines[0].split(" ", 1)[1])
        header["sha256"] = "0" * 64
        lines[0] = "MINILOOP_EVIDENCE_BEGIN " + json.dumps(header)
        self.assert_rejected_without_destination(lines)

    def attacker_envelope(self, entries: list[tarfile.TarInfo]) -> list[str]:
        # An attacker can recompute a transport checksum. These archives are
        # correctly framed/authenticated data and still must fail tar admission.
        raw = io.BytesIO()
        with tarfile.open(fileobj=raw, mode="w:gz") as archive:
            for entry in entries:
                if entry.isfile():
                    payload = b"attacker-controlled-file"
                    entry.size = len(payload)
                    archive.addfile(entry, io.BytesIO(payload))
                else:
                    archive.addfile(entry)
        payload = raw.getvalue()
        ident = "1234567890abcdef1234567890abcdef"
        chunks = [payload[index:index + 12288] for index in range(0, len(payload), 12288)]
        header = {"id": ident, "bytes": len(payload), "chunks": len(chunks),
                  "sha256": hashlib.sha256(payload).hexdigest()}
        return (["MINILOOP_EVIDENCE_BEGIN " + json.dumps(header)]
                + [f"MINILOOP_EVIDENCE_CHUNK {ident} {index} " + base64.b64encode(chunk).decode("ascii")
                   for index, chunk in enumerate(chunks)]
                + ["MINILOOP_EVIDENCE_END " + ident])

    def test_tar_parent_and_absolute_paths_do_not_escape_or_publish(self):
        outside = self.root / "escaped"
        for index, name in enumerate(("../escaped", "nested/../../escaped", str(outside))):
            with self.subTest(name=name):
                self.assert_rejected_without_destination(self.attacker_envelope([tarfile.TarInfo(name)]), f"unsafe-{index}")
                self.assertFalse(outside.exists())

    def test_tar_symbolic_and_hard_links_are_rejected_without_publication(self):
        for index, kind in enumerate((tarfile.SYMTYPE, tarfile.LNKTYPE)):
            with self.subTest(kind=kind):
                link = tarfile.TarInfo("linked")
                link.type = kind
                link.linkname = "../escaped"
                self.assert_rejected_without_destination(self.attacker_envelope([link]), f"link-{index}")
                self.assertFalse((self.root / "escaped").exists())

    def test_tar_duplicate_member_is_rejected_without_publication(self):
        self.assert_rejected_without_destination(self.attacker_envelope([
            tarfile.TarInfo("same.txt"), tarfile.TarInfo("same.txt")]))

    def test_existing_directory_and_file_are_not_overwritten(self):
        _, _, lines = self.valid_evidence()
        log = self.log_file(lines)
        directory = self.root / "existing-directory"
        directory.mkdir()
        marker = directory / "keep"
        marker.write_bytes(b"original directory bytes")
        file = self.root / "existing-file"
        file.write_bytes(b"original file bytes")
        for destination in (directory, file):
            with self.subTest(destination=destination.name), self.assertRaises(ValueError):
                transport.extract(log, destination)
        self.assertEqual(marker.read_bytes(), b"original directory bytes")
        self.assertEqual(file.read_bytes(), b"original file bytes")

    def test_existing_dangling_symlink_is_not_overwritten(self):
        _, _, lines = self.valid_evidence()
        destination = self.root / "existing-dangling-link"
        destination.symlink_to(self.root / "missing-target", target_is_directory=True)
        with self.assertRaises(ValueError):
            transport.extract(self.log_file(lines), destination)
        self.assertTrue(destination.is_symlink())
        self.assertFalse((self.root / "missing-target").exists())


if __name__ == "__main__":
    unittest.main()
