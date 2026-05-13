/**
 * ProxiPort REST API client.
 *
 * Handles JWT bearer auth, JSON envelopes ({"data": ...}), error envelopes
 * ({"errors": [{"code", "title", "detail"}]}), and the two-step login flow
 * (POST /login then POST /verify-2fa with the TOTP code).
 */
import { tokenStore, vaultStatus, type VaultStatusShape } from './stores';
import { get } from 'svelte/store';

export type ApiError = { code?: string; title: string; detail?: string };

export class ApiException extends Error {
  status: number;
  errors: ApiError[];
  constructor(status: number, errors: ApiError[]) {
    super(errors[0]?.title || `HTTP ${status}`);
    this.status = status;
    this.errors = errors;
  }
}

/**
 * Refuse to issue any authenticated request when we don't have a token.
 * The server's BanList adds the caller (keyed by username, with `""` for
 * un-authed) to a 2-second deny-list on every failed auth; if the SPA
 * fires three parallel API calls after a session expires, the first
 * gets 401 and the rest cascade into 429 "too many requests". Throwing
 * an ApiException(401) here short-circuits that storm and lets the
 * layout's reactive redirect take the user back to /auth cleanly.
 */
function ensureToken(): string {
  const t = get(tokenStore);
  if (!t) {
    throw new ApiException(401, [{ title: 'not authenticated' }]);
  }
  return t;
}

function authHeader(token?: string | null): Record<string, string> {
  const t = token ?? get(tokenStore);
  return t ? { Authorization: `Bearer ${t}` } : {};
}

async function parseJson(res: Response): Promise<any> {
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('application/json')) {
    return await res.json();
  }
  return null;
}

async function raise(res: Response): Promise<never> {
  const body = await parseJson(res);
  const errors: ApiError[] = body?.errors ?? [{ title: res.statusText || `HTTP ${res.status}` }];
  if (res.status === 401) {
    tokenStore.set(null);
  }
  throw new ApiException(res.status, errors);
}

/** GET /api/v1/<path> with bearer auth. Returns parsed `data` field, or full body if no envelope. */
export async function apiGet<T = any>(path: string): Promise<T> {
  ensureToken();
  const res = await fetch(`/api/v1${path}`, { headers: { ...authHeader() } });
  if (!res.ok) await raise(res);
  const body = await parseJson(res);
  return (body && 'data' in body ? body.data : body) as T;
}

/**
 * Normalize a list endpoint's payload to an array. A few list endpoints
 * (e.g. `/schedules` pre-fix) double-wrap as `{data:{data:[],meta:{}}}`,
 * so apiGet returns the inner envelope object instead of the array. This
 * helper accepts either shape (array, `{data:[...]}`, null/undefined) and
 * yields an array.
 */
export function asList<T>(v: unknown): T[] {
  if (Array.isArray(v)) return v as T[];
  if (v && typeof v === 'object' && Array.isArray((v as { data?: unknown }).data)) {
    return (v as { data: T[] }).data;
  }
  return [];
}

export async function apiPost<T = any>(path: string, payload?: unknown): Promise<T> {
  ensureToken();
  const res = await fetch(`/api/v1${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeader() },
    body: payload === undefined ? undefined : JSON.stringify(payload)
  });
  if (!res.ok) await raise(res);
  const body = await parseJson(res);
  return (body && 'data' in body ? body.data : body) as T;
}

export async function apiPut<T = any>(path: string, payload?: unknown): Promise<T> {
  ensureToken();
  const res = await fetch(`/api/v1${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...authHeader() },
    body: payload === undefined ? undefined : JSON.stringify(payload)
  });
  if (!res.ok) await raise(res);
  const body = await parseJson(res);
  return (body && 'data' in body ? body.data : body) as T;
}

export async function apiDelete(path: string): Promise<void> {
  ensureToken();
  const res = await fetch(`/api/v1${path}`, { method: 'DELETE', headers: { ...authHeader() } });
  if (!res.ok) await raise(res);
}

/** POST a multipart form (used by the per-client Files push form). */
export async function apiPostForm<T = any>(path: string, form: FormData): Promise<T> {
  ensureToken();
  const res = await fetch(`/api/v1${path}`, {
    method: 'POST',
    headers: { ...authHeader() }, // do NOT set Content-Type, browser handles boundary
    body: form
  });
  if (!res.ok) await raise(res);
  const body = await parseJson(res);
  return (body && 'data' in body ? body.data : body) as T;
}

/**
 * Step 1 of login. With TOTP enabled, the response carries
 * `data.two_fa.token` (a one-time token) and `data.two_fa.send_to`
 * but no full JWT yet — we then call verify2fa with the TOTP code
 * to get a usable JWT.
 *
 * With TOTP disabled, `data.token` is the full JWT and we're done.
 */
export async function login(username: string, password: string): Promise<LoginResponse> {
  const basic = btoa(`${username}:${password}`);
  // /login is a GET with HTTP-basic auth (see api-doc/openapi/paths/login.yaml).
  // ?token-lifetime extends the server-side session cache TTL — without it,
  // bearer.DefaultTokenLifetime (10 minutes) kicks in and an idle tab will
  // start cascading 401s into rate-limit bans on its next render.
  const res = await fetch(`/api/v1/login?token-lifetime=${SESSION_LIFETIME_SECONDS}`, {
    method: 'GET',
    headers: { Authorization: `Basic ${basic}` }
  });
  if (!res.ok) await raise(res);
  const body = await parseJson(res);
  return body.data as LoginResponse;
}

const SESSION_LIFETIME_SECONDS = 24 * 60 * 60; // 24 hours

/** After 401 we want to send the user to /auth (the login route) rather than /login. */
export const LOGIN_PATH = '/auth';

export type LoginResponse = {
  token?: string;
  two_fa?: { token: string; send_to?: string; delivery_method?: string };
};

/**
 * Step 2 of login. POST /verify-2fa with `{username, token: totpCode}` and
 * the short-lived login token as a Bearer header to get the final JWT.
 *
 * The server (api_handler_verify2fa.go) requires the Bearer token when
 * TotP is enabled — it identifies which user the TOTP code is being
 * verified for.
 */
export async function verify2fa(username: string, totp: string, loginToken: string): Promise<string> {
  const res = await fetch(`/api/v1/verify-2fa?token-lifetime=${SESSION_LIFETIME_SECONDS}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${loginToken}`
    },
    body: JSON.stringify({ username, token: totp })
  });
  if (!res.ok) await raise(res);
  const body = await parseJson(res);
  return body.data?.token as string;
}

export async function logout(): Promise<void> {
  try {
    await apiDelete('/logout');
  } catch (_) {
    /* even if it fails we wipe the local token */
  }
  tokenStore.set(null);
}

/**
 * Fetch the live vault status from the server and update the `vaultStatus`
 * store. Pages that gate on the vault should call this so their state
 * tracks the server, not a per-session SPA flag.
 *
 * The endpoint always returns 200 (with init: "uninitialized" when the
 * vault has never been set up); 401 means we lost auth.
 */
export async function refreshVaultStatus(): Promise<VaultStatusShape> {
  const s = await apiGet<VaultStatusShape>('/vault-admin');
  const next: VaultStatusShape = {
    init: (s?.init ?? 'unknown') as VaultStatusShape['init'],
    status: (s?.status ?? '') as VaultStatusShape['status']
  };
  vaultStatus.set(next);
  return next;
}

/** Build a websocket URL with the JWT in the access_token query parameter. */
export function wsUrl(path: string): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const t = get(tokenStore);
  const sep = path.includes('?') ? '&' : '?';
  return `${proto}//${location.host}/api/v1${path}${sep}access_token=${encodeURIComponent(t || '')}`;
}
