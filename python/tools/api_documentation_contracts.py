"""Capture actual default FastAPI documentation routes and their admission rules."""
from __future__ import annotations

import asyncio
import hashlib
import inspect
from pathlib import Path

import fastapi
import httpx
from fastapi.openapi import docs

from mini_loop.auth import NullAuth, TokenAuth
from mini_loop.server import create_app

DOCUMENTS = (
    ('/openapi.json', 'openapi.json'),
    ('/docs', 'swagger.html'),
    ('/docs/oauth2-redirect', 'oauth2-redirect.html'),
    ('/redoc', 'redoc.html'),
)


async def _documentation_assets() -> dict[str, bytes]:
    app = create_app()
    app.state.auth = NullAuth()
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url='http://fixture') as client:
        assets = {}
        for path, filename in DOCUMENTS:
            response = await client.get(path)
            response.raise_for_status()
            assets[filename] = response.content
        return assets


def documentation_assets() -> dict[str, bytes]:
    return asyncio.run(_documentation_assets())


async def _documentation_contracts() -> dict:
    app = create_app()
    cases, assets = [], []
    profiles = (
        ('anonymous-get', False, 'GET', '', '', ''),
        ('anonymous-head', False, 'HEAD', '', '', ''),
        ('token-get', True, 'GET', 'Bearer fixture-token', '', ''),
        ('missing-token', True, 'GET', '', '', ''),
        ('missing-token-head', True, 'HEAD', '', '', ''),
        ('query-token', True, 'GET', '', '?access_token=fixture-token', ''),
        ('unsupported-method', True, 'POST', 'Bearer fixture-token', '', ''),
        ('trailing-slash', True, 'GET', 'Bearer fixture-token', '/', ''),
        ('oversized-body', True, 'GET', 'Bearer fixture-token', '', 'oversized'),
    )
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url='http://fixture') as client:
        for path, filename in DOCUMENTS:
            for name, authenticated, method, credential, suffix, body_kind in profiles:
                app.state.auth = TokenAuth({'fixture-token': 'owner'}) if authenticated else NullAuth()
                headers = {'Authorization': credential} if credential else {}
                body = b'x' * (10 * 1024 * 1024 + 1) if body_kind else b''
                response = await client.request(method, path + suffix, headers=headers, content=body)
                observed = {key: response.headers[key] for key in (
                    'content-type', 'content-length', 'content-security-policy',
                    'x-content-type-options', 'x-frame-options', 'referrer-policy',
                    'www-authenticate', 'location',
                ) if key in response.headers}
                allowed = sorted(part.strip() for part in response.headers.get('allow', '').split(',') if part.strip())
                cases.append(dict(name=name, path=path + suffix, method=method,
                                  token_auth=authenticated, authorization=credential,
                                  body_kind=body_kind, status=response.status_code,
                                  headers=observed, allowed_methods=allowed,
                                  body_json=response.json() if response.status_code >= 400 and response.content else None,
                                  body_sha256=hashlib.sha256(response.content).hexdigest()))
                if name == 'anonymous-get':
                    assets.append(dict(path=path, filename=filename,
                                       bytes=len(response.content),
                                       media_type=response.headers['content-type'],
                                       sha256=hashlib.sha256(response.content).hexdigest()))
    return dict(fastapi_version=fastapi.__version__,
                docs_source_sha256=hashlib.sha256(Path(inspect.getfile(docs)).read_bytes()).hexdigest(),
                assets=assets, cases=cases)


def api_documentation_contracts() -> dict:
    return asyncio.run(_documentation_contracts())
