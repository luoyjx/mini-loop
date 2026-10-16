"""Opt-in, local fault prototype; not a production Task control plane.

Pinned seams: temporalio 1.34.0 and deepagents 0.7.25. SQLite is shared only
between local prototype processes. It is not a distributed or HA memory store.
"""

CODE_VERSION = "prototype-v1"
SNAPSHOT_SCHEMA = 1
