/**
 * Base64 and ACL helpers.
 *
 * These live here rather than inside a page because more than one page needs
 * them and, when a page kept its own copy, the copies disagreed: the scripts
 * page encoded a script body correctly while the schedules page sent the raw
 * textarea contents to an endpoint that base64-decodes it.
 */

/**
 * Base64-encode a UTF-8 string.
 *
 * btoa() alone throws on any code point above 255 -- smart quotes, emoji,
 * accented or CJK characters are all routinely pasted into a script or typed
 * into a password -- and for code points 128-255 it silently encodes the
 * single Latin-1 byte rather than the UTF-8 pair, so the far end sees
 * different bytes than the user typed. Encoding to UTF-8 first fixes both.
 *
 * Chunked, because String.fromCharCode(...bytes) on a large script overflows
 * the call stack.
 */
export function utf8ToBase64(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let binary = '';
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

/**
 * Decode a base64 string back to UTF-8, returning null if it is not valid
 * base64.
 *
 * The null case matters: a schedule created before the encoding was fixed
 * holds a raw script body, and loading it into the edit form has to show that
 * body rather than throwing or displaying mojibake.
 */
export function base64ToUtf8(s: string): string | null {
  try {
    const binary = atob(s);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    return null;
  }
}

/**
 * The ACL entry for a single host.
 *
 * The tunnel form appended a literal "/32" to whatever GET /me/ip returned.
 * For an IPv4 operator that is one host; for an IPv6 operator it is a /32
 * IPv6 prefix -- 2^96 addresses, an ISP-sized block -- and the server accepts
 * it silently, because net.ParseCIDR is perfectly happy with an IPv6 /32. The
 * UI said "Only my current IP address" while opening the tunnel to millions of
 * unrelated hosts.
 *
 * A single host is /32 in IPv4 and /128 in IPv6.
 */
export function singleHostAcl(ip: string): string {
  const addr = ip.trim();
  if (!addr) return '';
  // An IPv6 literal is the only address form containing a colon; a zone or a
  // bracketed form would not survive as an ACL entry anyway.
  return addr.includes(':') ? `${addr}/128` : `${addr}/32`;
}
