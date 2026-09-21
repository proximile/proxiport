import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

import {
  apiGet,
  apiPost,
  asList,
  login,
  verify2fa,
  logout,
  openAuthedSocket,
  wsUrl,
  ApiException,
  LOGIN_PATH
} from './api';
import { tokenStore } from './stores';

/** A Response stand-in good enough for the client's parseJson/raise paths. */
function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: `HTTP ${status}`,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => body
  } as unknown as Response;
}

function emptyResponse(status = 204): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: `HTTP ${status}`,
    headers: new Headers(),
    json: async () => null
  } as unknown as Response;
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  tokenStore.set(null);
  localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the unauthenticated short-circuit', () => {
  // The server bans by username (with "" for un-authed) for two seconds on
  // every failed auth, so three parallel calls after a session expires turn
  // one 401 into a 429 storm. The client refuses to issue the request at all.
  it('throws 401 without issuing a request when there is no token', async () => {
    await expect(apiGet('/clients')).rejects.toBeInstanceOf(ApiException);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('short-circuits writes too, not just reads', async () => {
    await expect(apiPost('/clients', { a: 1 })).rejects.toBeInstanceOf(ApiException);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('reports the short-circuit as a 401 so the layout redirect fires', async () => {
    const err = await apiGet('/clients').catch((e) => e);
    expect(err).toBeInstanceOf(ApiException);
    expect((err as ApiException).status).toBe(401);
  });
});

describe('bearer auth', () => {
  it('sends the token as a Bearer header', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(jsonResponse({ data: [] }));

    await apiGet('/clients');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/clients');
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer jwt-abc');
  });

  it('unwraps the data envelope', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(jsonResponse({ data: { id: 'c1' } }));

    await expect(apiGet('/clients/c1')).resolves.toEqual({ id: 'c1' });
  });
});

describe('session invalidation', () => {
  it('clears the stored token on 401', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(jsonResponse({ errors: [{ title: 'expired' }] }, 401));

    await expect(apiGet('/clients')).rejects.toBeInstanceOf(ApiException);
    expect(get(tokenStore)).toBeNull();
  });

  // The vault answers a wrong master passphrase with 401. Treating that as a
  // dead session signed the operator out of the whole console -- back through
  // username, password and a TOTP code -- for mistyping an unrelated secret.
  // And because this path never calls DELETE /logout, the discarded JWT stayed
  // valid server-side for its full lifetime: signed out in the UI only.
  it('keeps the token when the vault rejects a passphrase with 401', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(
      jsonResponse({ errors: [{ title: 'wrong password provided' }] }, 401)
    );

    await expect(apiPost('/vault-admin/sesame', { password: 'wrong' })).rejects.toBeInstanceOf(
      ApiException
    );
    expect(get(tokenStore)).toBe('jwt-abc');
  });

  // The exemption is for that one endpoint, not for anything vault-shaped.
  it('still clears the token on a 401 from another vault endpoint', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(jsonResponse({ errors: [{ title: 'expired' }] }, 401));

    await expect(apiGet('/vault-admin')).rejects.toBeInstanceOf(ApiException);
    expect(get(tokenStore)).toBeNull();
  });

  // A 403 means "authenticated but not permitted". Treating it as a session
  // failure would log a user out of the whole SPA for opening one page their
  // group cannot see.
  it('keeps the token on 403', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(jsonResponse({ errors: [{ title: 'forbidden' }] }, 403));

    await expect(apiGet('/clients')).rejects.toBeInstanceOf(ApiException);
    expect(get(tokenStore)).toBe('jwt-abc');
  });

  it('keeps the token on 500', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(jsonResponse({ errors: [{ title: 'boom' }] }, 500));

    await expect(apiGet('/clients')).rejects.toBeInstanceOf(ApiException);
    expect(get(tokenStore)).toBe('jwt-abc');
  });

  it('surfaces the server error envelope', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(
      jsonResponse({ errors: [{ code: 'x', title: 'nope', detail: 'because' }] }, 400)
    );

    const err = (await apiGet('/clients').catch((e) => e)) as ApiException;
    expect(err.status).toBe(400);
    expect(err.errors[0]).toEqual({ code: 'x', title: 'nope', detail: 'because' });
  });
});

describe('login', () => {
  it('uses basic auth and asks for a long session', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ data: { token: 'jwt-new' } }));

    await login('alice', 'hunter2');

    const [url, init] = fetchMock.mock.calls[0];
    expect(init.method).toBe('GET');
    // Without token-lifetime the server default (10 minutes) makes an idle tab
    // cascade 401s into rate-limit bans.
    expect(String(url)).toContain('token-lifetime=');
    expect((init.headers as Record<string, string>).Authorization).toBe(
      `Basic ${btoa('alice:hunter2')}`
    );
  });

  it('does not require a stored token to log in', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ data: { token: 'jwt-new' } }));
    await expect(login('alice', 'hunter2')).resolves.toEqual({ token: 'jwt-new' });
  });

  it('carries the interim login token as Bearer when verifying the second factor', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ data: { token: 'jwt-final' } }));

    await expect(verify2fa('alice', '123456', 'interim-token')).resolves.toBe('jwt-final');

    const [, init] = fetchMock.mock.calls[0];
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer interim-token');
    expect(JSON.parse(init.body as string)).toEqual({ username: 'alice', token: '123456' });
  });

  it('sends users to the login route, not /login', () => {
    // /login is the API endpoint; /auth is the SPA route.
    expect(LOGIN_PATH).toBe('/auth');
  });
});

describe('logout', () => {
  it('wipes the local token even when the server call fails', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockRejectedValue(new Error('network down'));

    await logout();

    expect(get(tokenStore)).toBeNull();
  });

  it('wipes the local token on a successful logout', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(emptyResponse(204));

    await logout();

    expect(get(tokenStore)).toBeNull();
  });
});

describe('authenticated websockets', () => {
  it('puts only the single-use ticket on the URL, never the JWT', async () => {
    tokenStore.set('super-secret-jwt');
    fetchMock.mockResolvedValue(jsonResponse({ data: { ticket: 'one-shot-ticket' } }));

    const seen: string[] = [];
    vi.stubGlobal(
      'WebSocket',
      class {
        constructor(url: string) {
          seen.push(url);
        }
      }
    );

    await openAuthedSocket('/clients/c1/updates');

    expect(seen).toHaveLength(1);
    // The whole point of the ticket exchange: a browser cannot set an
    // Authorization header on a WS handshake, and a JWT on the URL would land
    // in access logs and browser history.
    expect(seen[0]).not.toContain('super-secret-jwt');
    expect(seen[0]).toContain('ticket=one-shot-ticket');
  });

  it('appends the ticket with & when the path already has a query', async () => {
    tokenStore.set('jwt-abc');
    fetchMock.mockResolvedValue(jsonResponse({ data: { ticket: 't' } }));

    const seen: string[] = [];
    vi.stubGlobal(
      'WebSocket',
      class {
        constructor(url: string) {
          seen.push(url);
        }
      }
    );

    await openAuthedSocket('/x?a=1');

    expect(seen[0]).toContain('?a=1&ticket=t');
  });

  it('builds a ws:// url for http and carries no auth material', () => {
    const u = wsUrl('/clients');
    expect(u.startsWith('ws://') || u.startsWith('wss://')).toBe(true);
    expect(u).toContain('/api/v1/clients');
    expect(u).not.toContain('ticket');
  });
});

describe('asList', () => {
  it('passes an array through', () => {
    expect(asList([1, 2])).toEqual([1, 2]);
  });

  // Some list endpoints double-wrap as {data:{data:[],meta:{}}}, so apiGet
  // hands back the inner envelope rather than the array.
  it('unwraps a nested data envelope', () => {
    expect(asList({ data: [1], meta: {} })).toEqual([1]);
  });

  it('yields an empty array for null, undefined and a bare object', () => {
    expect(asList(null)).toEqual([]);
    expect(asList(undefined)).toEqual([]);
    expect(asList({})).toEqual([]);
  });

  it('does not throw on a string', () => {
    expect(asList('nope')).toEqual([]);
  });
});

describe('basic-auth encoding on login', () => {
  // btoa() throws above U+00FF and silently sends the Latin-1 byte below it,
  // so a password with any non-ASCII character could never be used to sign in.
  it('sends a UTF-8 base64 credential, not a Latin-1 one', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ data: { token: 'jwt' } }));

    await login('operator', 'pässwörd');

    const [, init] = fetchMock.mock.calls[0];
    const header = (init.headers as Record<string, string>).Authorization;
    const decoded = new TextDecoder().decode(
      Uint8Array.from(atob(header.replace('Basic ', '')), (c) => c.charCodeAt(0))
    );
    expect(decoded).toBe('operator:pässwörd');
  });

  it('does not throw on a password outside Latin-1', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ data: { token: 'jwt' } }));
    await expect(login('operator', '🔐€')).resolves.toBeDefined();
  });
});
