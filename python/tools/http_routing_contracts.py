"""Observe default Source routing without invoking any business handler."""
from __future__ import annotations

import asyncio
import re
from urllib.parse import unquote

import fastapi
import httpx
import starlette

from mini_loop.auth import TokenAuth
from mini_loop.server import create_app


async def _http_routing_contracts() -> dict:
    app = create_app()
    app.state.auth = TokenAuth({'fixture-token': 'owner'})
    inventory = {}
    for route in app.routes:
        if route.path in ('/openapi.json', '/docs', '/docs/oauth2-redirect', '/redoc'):
            continue
        inventory.setdefault(route.path, []).append(sorted(route.methods))
    cases = []
    exact_path = None

    async def preserve_path(scope, receive, send):
        if exact_path is not None:
            raw = exact_path.split('?', 1)[0]
            scope = dict(scope, path=unquote(raw), raw_path=raw.encode())
        await app(scope, receive, send)

    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=preserve_path), base_url='http://fixture') as client:
        async def observe(name, path, *, credential='Bearer fixture-token', body=b'', exact=False):
            nonlocal exact_path
            exact_path = path if exact else None
            headers = {'Authorization': credential} if credential else {}
            response = await client.request('TRACE', path, headers=headers, content=body)
            cases.append(dict(name=name, method='TRACE', path=path,
                              authorization=credential, oversized_body=bool(body),
                              status=response.status_code,
                              allowed_methods=sorted(part.strip() for part in response.headers.get('allow', '').split(',') if part.strip()),
                              location=response.headers.get('location'),
                              body_json=response.json() if response.content else None,
                              security_headers={key: response.headers[key] for key in (
                                  'content-security-policy', 'x-content-type-options',
                                  'x-frame-options', 'referrer-policy') if key in response.headers}))
        for pattern in inventory:
            path = re.sub(r'\{[^}]+\}', 'fixture-id', pattern)
            await observe('unsupported-method', path)
            if path != '/':
                await observe('trailing-slash', path + '/?probe=keep')
                await observe('missing-token-slash', path + '/', credential='')
                await observe('query-token-slash', path + '/?access_token=fixture-token', credential='')
        await observe('unknown-path', '/fixture-missing')
        await observe('unknown-missing-token', '/fixture-missing', credential='')
        await observe('oversized-before-redirect', '/sessions/', credential='', body=b'x' * (10 * 1024 * 1024 + 1))
        for name, path in (
            ('dot-current', '/sessions/.'),
            ('dot-parent', '/sessions/..'),
            ('nested-parent', '/sessions/fixture-id/../cron'),
            ('repeated-trailing', '/sessions///?probe=keep'),
            ('encoded-slash', '/sessions/fixture%2Fid'),
            ('encoded-dot', '/sessions/%2E%2E'),
            ('empty-segment', '/sessions//fixture-id'),
            ('unknown-slash', '/fixture-missing/?probe=keep'),
        ):
            # httpx normalizes literal dot segments. Preserve the requested ASGI
            # path at the transport boundary; all middleware/router code is real.
            await observe(name, path, exact=True)
    return dict(fastapi_version=fastapi.__version__, starlette_version=starlette.__version__,
                routes=[dict(path=path, ordered_methods=methods) for path, methods in inventory.items()],
                cases=cases)


def http_routing_contracts() -> dict:
    return asyncio.run(_http_routing_contracts())
