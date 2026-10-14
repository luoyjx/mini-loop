#!/usr/bin/env python3
"""Exercise the real Linux pressure guard with small, entirely mocked proc data."""
from __future__ import annotations

import importlib.util
from pathlib import Path
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("go_port_host_pressure", Path(__file__).with_name("host_pressure.py"))
pressure = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(pressure)

GIB = 1024 * 1024 * 1024
NORMAL_MEMORY = "MemTotal: 16777216 kB\nMemFree: 1024 kB\nMemAvailable: 8388608 kB\nCached: 2048 kB\n"
NORMAL_PSI = "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n"


class HostPressureTests(unittest.TestCase):
    def observe(self, memory: str = NORMAL_MEMORY, psi: str = NORMAL_PSI) -> dict:
        texts = {"/proc/meminfo": memory, "/proc/pressure/memory": psi}
        reads = []

        def read_fixture(path):
            name = path.as_posix()
            self.assertIn(name, texts, "guard attempted an unmocked file read")
            reads.append(name)
            return texts[name]

        # Path construction is inert; every read_text call is intercepted before
        # it can touch the host. No actual /proc or subprocess is used.
        with mock.patch.object(pressure.Path, "read_text", autospec=True, side_effect=read_fixture):
            try:
                return pressure.observe()
            finally:
                self.assertEqual(reads, ["/proc/meminfo", "/proc/pressure/memory"])

    def assert_invalid_observation(self, memory: str, psi: str) -> None:
        with self.assertRaises(pressure.PressureReadError) as captured:
            self.observe(memory, psi)
        observation = captured.exception.observation
        self.assertEqual(observation["status"], "unavailable")
        self.assertEqual(observation["meminfo_raw"], memory)
        self.assertEqual(observation["memory_psi_raw"], psi)
        self.assertEqual(observation["policy"], pressure.POLICY)
        self.assertTrue(observation["error"])
        self.assertEqual(str(captured.exception), observation["error"])

    def test_normal_16g_host_with_8g_available_and_no_stalls(self):
        observed = self.observe()
        self.assertEqual(observed["status"], "normal")
        self.assertEqual(observed["reasons"], [])
        self.assertEqual(observed["mem_total_bytes"], 16 * GIB)
        self.assertEqual(observed["mem_available_bytes"], 8 * GIB)
        self.assertEqual(observed["required_available_bytes"], 6 * GIB)
        self.assertEqual(observed["memory_psi"]["full"]["avg10"], 0)
        self.assertEqual(observed["memory_psi"]["some"]["avg10"], 0)
        self.assertEqual(observed["meminfo_raw"], NORMAL_MEMORY)
        self.assertEqual(observed["memory_psi_raw"], NORMAL_PSI)

    def test_available_memory_below_6g_warns_and_exact_boundary_passes(self):
        below = NORMAL_MEMORY.replace("MemAvailable: 8388608", "MemAvailable: 6291455")
        observed = self.observe(below)
        self.assertEqual(observed["status"], "warning")
        self.assertEqual(observed["reasons"], ["host_available_memory_below_minimum"])
        self.assertEqual(observed["mem_available_bytes"], 6 * GIB - 1024)
        self.assertEqual(observed["meminfo_raw"], below)
        boundary = NORMAL_MEMORY.replace("MemAvailable: 8388608", "MemAvailable: 6291456")
        self.assertEqual(self.observe(boundary)["status"], "normal")

    def test_any_full_avg10_stall_warns(self):
        psi = NORMAL_PSI.replace("full avg10=0.00", "full avg10=0.01")
        observed = self.observe(psi=psi)
        self.assertEqual(observed["status"], "warning")
        self.assertEqual(observed["reasons"], ["host_full_memory_stall"])
        self.assertEqual(observed["memory_psi_raw"], psi)

    def test_some_avg10_above_5_warns_and_exact_boundary_passes(self):
        psi = NORMAL_PSI.replace("some avg10=0.00", "some avg10=5.01")
        observed = self.observe(psi=psi)
        self.assertEqual(observed["status"], "warning")
        self.assertEqual(observed["reasons"], ["host_some_memory_stall_above_limit"])
        self.assertEqual(observed["memory_psi_raw"], psi)
        boundary = NORMAL_PSI.replace("some avg10=0.00", "some avg10=5.00")
        self.assertEqual(self.observe(psi=boundary)["status"], "normal")

    def test_missing_or_duplicate_memavailable_is_not_a_normal_sample(self):
        missing = "".join(line for line in NORMAL_MEMORY.splitlines(keepends=True) if not line.startswith("MemAvailable:"))
        duplicate = NORMAL_MEMORY + "MemAvailable: 8388608 kB\n"
        for memory in (missing, duplicate):
            with self.subTest(memory=memory):
                self.assert_invalid_observation(memory, NORMAL_PSI)

    def test_missing_or_duplicate_psi_rows_preserve_raw_failure(self):
        some, full = NORMAL_PSI.splitlines(keepends=True)
        for name, psi in (("missing-full", some), ("missing-some", full),
                          ("duplicate-some", some + full + some), ("duplicate-full", some + full + full)):
            with self.subTest(name=name):
                self.assert_invalid_observation(NORMAL_MEMORY, psi)

    def test_nonfinite_averages_and_memory_amounts_are_rejected(self):
        corrupt_psi = (
            NORMAL_PSI.replace("some avg10=0.00", "some avg10=NaN"),
            NORMAL_PSI.replace("full avg10=0.00", "full avg10=inf"),
            NORMAL_PSI.replace("avg60=0.00", "avg60=-inf", 1),
            NORMAL_PSI.replace("avg300=0.00", "avg300=nan", 1),
        )
        for psi in corrupt_psi:
            with self.subTest(psi=psi):
                self.assert_invalid_observation(NORMAL_MEMORY, psi)
        for value in ("NaN", "inf"):
            with self.subTest(memory_value=value):
                memory = NORMAL_MEMORY.replace("MemAvailable: 8388608", "MemAvailable: " + value)
                self.assert_invalid_observation(memory, NORMAL_PSI)

    def test_failed_psi_read_keeps_the_preceding_meminfo_observation(self):
        reads = []

        def read_fixture(path):
            name = path.as_posix()
            reads.append(name)
            if name == "/proc/meminfo":
                return NORMAL_MEMORY
            self.assertEqual(name, "/proc/pressure/memory")
            raise FileNotFoundError("mock PSI is missing")

        with mock.patch.object(pressure.Path, "read_text", autospec=True, side_effect=read_fixture):
            with self.assertRaises(pressure.PressureReadError) as captured:
                pressure.observe()
        self.assertEqual(reads, ["/proc/meminfo", "/proc/pressure/memory"])
        observed = captured.exception.observation
        self.assertEqual(observed["status"], "unavailable")
        self.assertEqual(observed["meminfo_raw"], NORMAL_MEMORY)
        self.assertNotIn("memory_psi_raw", observed)
        self.assertIn("FileNotFoundError", observed["error"])


if __name__ == "__main__":
    unittest.main()
