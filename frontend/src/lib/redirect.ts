/** Where to send a user after signing in when no return path was asked for. */
export const DEFAULT_LANDING = '/inventory';

/**
 * Build the sign-in URL that brings the user back to `path` afterwards.
 * `path` is the pathname plus query of the page they were on.
 */
export function loginUrl(path: string): string {
  if (!path || path === '/' || path.startsWith('/auth')) return '/auth';
  return `/auth?next=${encodeURIComponent(path)}`;
}

/**
 * Resolve the `next` query parameter of the sign-in page to a path inside this
 * app. Only same-origin relative paths are accepted; anything else (absolute
 * or protocol-relative URLs, backslash tricks, the sign-in page itself) falls
 * back to the default landing page.
 */
export function safeNext(next: string | null | undefined, origin: string): string {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) {
    return DEFAULT_LANDING;
  }
  try {
    const u = new URL(next, origin);
    // Dot segments can collapse to a leading "//" ("/.//host"), which a
    // browser would read as another host.
    if (
      u.origin !== origin ||
      u.pathname.startsWith('//') ||
      u.pathname === '/auth' ||
      u.pathname.startsWith('/auth/')
    ) {
      return DEFAULT_LANDING;
    }
    return u.pathname + u.search + u.hash;
  } catch {
    return DEFAULT_LANDING;
  }
}
