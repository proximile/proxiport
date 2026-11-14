/**
 * Stored tunnels (the library on a client's Tunnels page) and their ACL.
 *
 * A stored tunnel has one `acl` column, a literal address list. The tunnel
 * form's "Only my current IP address" is not an address, though: it means
 * "whoever opens this tunnel, from wherever they are at that moment". Saving
 * only the address that was current while saving turned that choice into a
 * fixed address, and using the entry later from another network created a
 * tunnel its own user could not reach.
 *
 * The choice is therefore kept beside the address, in the entry's free-form
 * `further_options`, and the address is looked up again whenever the entry
 * is used. The literal address stays in `acl` as well, so anything that
 * reads the entry without knowing the marker behaves as it did before.
 */
import { singleHostAcl } from './encoding';

/** `further_options.acl_mode` of an entry whose ACL is its user's own address. */
export const ACL_MODE_CURRENT = 'current';

export interface StoredTunnelAcl {
  acl?: string | null;
  further_options?: Record<string, unknown> | null;
}

/** True when the entry was saved with "Only my current IP address". */
export function storedAclFollowsUser(s: StoredTunnelAcl): boolean {
  return s.further_options?.acl_mode === ACL_MODE_CURRENT;
}

/** What the library table shows in its ACL column. */
export function storedAclLabel(s: StoredTunnelAcl): string {
  if (storedAclFollowsUser(s)) return 'My current IP address';
  return s.acl || '—';
}

/**
 * The ACL to send when a tunnel is created from a stored entry.
 *
 * `lookupIp` returns the caller's address as the server sees it. When the
 * entry follows its user and the address cannot be determined, this throws
 * instead of returning an empty ACL: an empty ACL means "no restriction",
 * the opposite of what was saved.
 */
export async function aclForStoredTunnel(
  s: StoredTunnelAcl,
  lookupIp: () => Promise<string>
): Promise<string> {
  if (!storedAclFollowsUser(s)) return s.acl || '';
  let ip = '';
  try {
    ip = (await lookupIp()).trim();
  } catch (err) {
    const why = err instanceof Error && err.message ? ` (${err.message})` : '';
    throw new Error(`your current IP address could not be determined${why}`);
  }
  if (!ip) {
    throw new Error('your current IP address could not be determined');
  }
  return singleHostAcl(ip);
}
