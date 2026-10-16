"""Test-only mTLS termination over a loopback plaintext Temporal backend.

This is transparent byte transport, not a gRPC implementation or a production
proxy. Audit records contain public certificate fingerprints, never traffic.
"""
from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import os
from pathlib import Path
import ssl
import time


def server_context(certificate: Path, private_key: Path, client_ca: Path) -> ssl.SSLContext:
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(certificate, private_key)
    context.load_verify_locations(cafile=client_ca)
    context.verify_mode = ssl.CERT_REQUIRED
    context.set_alpn_protocols(["h2"])
    return context


def peer_metadata(transport) -> dict:
    tls = transport.get_extra_info("ssl_object")
    if tls is None or tls.selected_alpn_protocol() != "h2" or not tls.getpeercert(binary_form=True):
        raise ValueError("required_mtls_h2_peer_missing")
    return {"client_certificate_sha256": hashlib.sha256(tls.getpeercert(binary_form=True)).hexdigest(),
            "alpn": tls.selected_alpn_protocol(), "tls_version": tls.version()}


async def copy_bytes(reader, writer) -> None:
    while chunk := await reader.read(65536):
        writer.write(chunk)
        await writer.drain()


async def serve(args) -> None:
    audit = {"schema_version": 1, "pid": os.getpid(), "connections": []}
    def persist():
        temporary = args.audit.with_suffix(".tmp")
        with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as stream:
            json.dump(audit, stream, sort_keys=True)
        temporary.replace(args.audit)
    async def accept(reader, writer):
        upstream = None
        pumps = []
        try:
            metadata = peer_metadata(writer)
            audit["connections"].append({**metadata, "connection_id": len(audit["connections"]) + 1,
                                         "accepted_monotonic_ns": time.monotonic_ns()})
            persist()
            remote, upstream = await asyncio.open_connection("127.0.0.1", args.backend_port)
            pumps = [asyncio.create_task(copy_bytes(reader, upstream)), asyncio.create_task(copy_bytes(remote, writer))]
            await asyncio.wait(pumps, return_when=asyncio.FIRST_COMPLETED)
        except (OSError, ValueError):
            pass  # Never log decrypted traffic or TLS error details.
        finally:
            for task in pumps:
                task.cancel()
            if pumps:
                await asyncio.gather(*pumps, return_exceptions=True)
            for endpoint in (upstream, writer):
                if endpoint is not None:
                    endpoint.close()
                    try:
                        await asyncio.wait_for(endpoint.wait_closed(), timeout=1)
                    except (OSError, TimeoutError):
                        pass
    context = server_context(args.certificate, args.private_key, args.client_ca)
    persist()
    server = await asyncio.start_server(accept, "127.0.0.1", args.port, ssl=context,
                                        ssl_handshake_timeout=3, reuse_address=True)
    print("TASK_TLS_TUNNEL_READY", flush=True)
    async with server:
        await server.serve_forever()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--backend-port", type=int, required=True)
    for field in ("certificate", "private-key", "client-ca", "audit"):
        parser.add_argument("--" + field, type=Path, required=True)
    args = parser.parse_args()
    if not all(1 <= value <= 65535 for value in (args.port, args.backend_port)):
        parser.error("ports must be in 1..65535")
    asyncio.run(serve(args))


if __name__ == "__main__":
    main()
