"""Read Linux memory pressure without environment, output or process side effects."""
from __future__ import annotations

import math
from pathlib import Path


POLICY = {
    'source': 'Linux /proc/meminfo and /proc/pressure/memory',
    'minimum_available_bytes': 6 * 1024 * 1024 * 1024,
    'minimum_available_fraction': 0.20,
    'maximum_full_avg10': 0.0,
    'maximum_some_avg10': 5.0,
    'read_failure': 'stop_without_launch_or_stop_owned_container',
}


class PressureReadError(RuntimeError):
    """A failed observation, including any raw inputs read before the failure."""

    def __init__(self, message: str, observation: dict):
        super().__init__(message)
        self.observation = observation


def observe() -> dict:
    """Return raw inputs, parsed values and normal/warning; failures raise."""
    row = {'policy': dict(POLICY), 'status': 'unavailable'}
    try:
        row['meminfo_raw'] = Path('/proc/meminfo').read_text()
        row['memory_psi_raw'] = Path('/proc/pressure/memory').read_text()
        memory = {}
        for line in row['meminfo_raw'].splitlines():
            if line.startswith(('MemTotal:', 'MemAvailable:')):
                name, amount, unit = line.split()
                if name in memory or unit != 'kB' or not amount.isdecimal():
                    raise ValueError('Malformed or duplicate host memory field')
                memory[name] = int(amount) * 1024
        total, available = memory['MemTotal:'], memory['MemAvailable:']
        if not 0 <= available <= total or total == 0:
            raise ValueError('Invalid host memory capacity')
        psi = {}
        for line in row['memory_psi_raw'].splitlines():
            parts = line.split()
            if not parts or parts[0] not in ('some', 'full') or parts[0] in psi:
                raise ValueError('Malformed or duplicate host PSI row')
            fields = {}
            for part in parts[1:]:
                name, value = part.split('=', 1)
                if name in fields:
                    raise ValueError('Duplicate host PSI field')
                fields[name] = value
            if set(fields) != {'avg10', 'avg60', 'avg300', 'total'} or not fields['total'].isdecimal():
                raise ValueError('Incomplete host PSI fields')
            averages = {key: float(fields[key]) for key in ('avg10', 'avg60', 'avg300')}
            if any(not math.isfinite(value) or not 0 <= value <= 100 for value in averages.values()):
                raise ValueError('Invalid host PSI average')
            psi[parts[0]] = dict(averages, total=int(fields['total']))
        if set(psi) != {'some', 'full'}:
            raise ValueError('Incomplete host PSI rows')
        required = max(POLICY['minimum_available_bytes'], (total + 4) // 5)
        reasons = []
        if available < required:
            reasons.append('host_available_memory_below_minimum')
        if psi['full']['avg10'] > POLICY['maximum_full_avg10']:
            reasons.append('host_full_memory_stall')
        if psi['some']['avg10'] > POLICY['maximum_some_avg10']:
            reasons.append('host_some_memory_stall_above_limit')
        row.update(mem_total_bytes=total, mem_available_bytes=available,
                   required_available_bytes=required, memory_psi=psi, reasons=reasons,
                   status='warning' if reasons else 'normal')
    except Exception as error:
        message = type(error).__name__ + ': ' + str(error)
        row['error'] = message
        raise PressureReadError(message, row) from error
    return row
