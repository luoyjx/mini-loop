"""Bounded one-shot HTTP proxy that proves an upstream response was lost."""

from __future__ import annotations

import asyncio
import hashlib
from typing import Any

import httpx

from tooling.acceptance.support.process import HarnessFailure


MAX_REQUEST_BYTES = 128 * 1024
MAX_RESPONSE_BYTES = 128 * 1024


async def _read_request(reader: asyncio.StreamReader) -> bytes:
    headers = await reader.readuntil(b"\r\n\r\n")
    if len(headers) > 16 * 1024:
        raise HarnessFailure("The response-loss request headers exceeded their bound")
    content_length = 0
    for line in headers.split(b"\r\n")[1:]:
        if line.lower().startswith(b"content-length:"):
            content_length = int(line.split(b":", 1)[1].strip())
    if content_length < 0 or len(headers) + content_length > MAX_REQUEST_BYTES:
        raise HarnessFailure("The response-loss request exceeded its bound")
    return headers + await reader.readexactly(content_length)


def _close_upstream_request(request: bytes, host: str, port: int) -> bytes:
    head, body = request.split(b"\r\n\r\n", 1)
    lines = head.split(b"\r\n")
    kept = [lines[0]]
    for line in lines[1:]:
        name = line.split(b":", 1)[0].lower()
        if name not in {b"connection", b"host"}:
            kept.append(line)
    kept.extend((f"Host: {host}:{port}".encode(), b"Connection: close"))
    return b"\r\n".join(kept) + b"\r\n\r\n" + body


async def post_and_drop_response(
    upstream_host: str,
    upstream_port: int,
    path: str,
    *,
    headers: dict[str, str],
    json_body: dict[str, Any],
    timeout: float = 10,
) -> dict[str, Any]:
    """Forward one POST, consume its complete response, then drop the client side."""
    loop = asyncio.get_running_loop()
    observed: asyncio.Future[dict[str, Any]] = loop.create_future()

    async def handle(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        upstream_writer: asyncio.StreamWriter | None = None
        try:
            request = await _read_request(reader)
            upstream_reader, upstream_writer = await asyncio.open_connection(upstream_host, upstream_port)
            upstream_writer.write(_close_upstream_request(request, upstream_host, upstream_port))
            await upstream_writer.drain()
            chunks = []
            size = 0
            while True:
                chunk = await upstream_reader.read(min(16 * 1024, MAX_RESPONSE_BYTES + 1 - size))
                if not chunk:
                    break
                chunks.append(chunk)
                size += len(chunk)
                if size > MAX_RESPONSE_BYTES:
                    raise HarnessFailure("The response-loss upstream response exceeded its bound")
            response = b"".join(chunks)
            status_line = response.split(b"\r\n", 1)[0].split()
            if len(status_line) < 2 or not status_line[1].isdigit():
                raise HarnessFailure("The response-loss upstream returned an invalid HTTP response")
            if not observed.done():
                observed.set_result({
                    "upstream_status": int(status_line[1]),
                    "upstream_response_bytes": len(response),
                    "upstream_response_sha256": hashlib.sha256(response).hexdigest(),
                })
        except BaseException as error:
            if not observed.done():
                observed.set_exception(error)
        finally:
            if upstream_writer is not None:
                upstream_writer.close()
                await upstream_writer.wait_closed()
            writer.close()
            await writer.wait_closed()

    server = await asyncio.start_server(handle, "127.0.0.1", 0, limit=MAX_REQUEST_BYTES)
    port = server.sockets[0].getsockname()[1]
    client_error: Exception | None = None
    try:
        async with httpx.AsyncClient(timeout=timeout, trust_env=False) as client:
            try:
                await client.post(f"http://127.0.0.1:{port}{path}", headers=headers, json=json_body)
            except httpx.TransportError as error:
                client_error = error
        result = await asyncio.wait_for(observed, timeout=timeout)
    finally:
        server.close()
        await server.wait_closed()
    if client_error is None:
        raise HarnessFailure("The response-loss client unexpectedly received an HTTP response")
    result["client_error"] = type(client_error).__name__
    return result
