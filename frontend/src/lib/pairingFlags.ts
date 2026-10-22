// The pairing service returns a two-line installer command with no switches;
// which switches to add is decided here, in the browser. These helpers build
// the permission switches (-x / -s / -r / -b), the Linux egress switches
// (-o / -e, the agent's `transports` chain) and splice switches into the
// command without touching the download line.

export interface InstallerOptions {
  /** -x: remote command and script execution. */
  x: boolean;
  /** -s: sudo rights for those commands; meaningless without -x. */
  s: boolean;
  /** -r: file reception. */
  r: boolean;
  /** -b: sudo rights for file reception; meaningless without -r. */
  b: boolean;
}

/**
 * The permission switches for one installer. A sudo switch is only emitted
 * together with the switch it extends, so a greyed-out option never grants
 * anything. The PowerShell installer knows only -x and -r.
 */
export function installerFlags(o: InstallerOptions, os: 'linux' | 'windows' = 'linux'): string {
  const linux = os === 'linux';
  return [o.x && '-x', linux && o.x && o.s && '-s', o.r && '-r', linux && o.r && o.b && '-b']
    .filter(Boolean)
    .join(' ');
}

/** The agent-host tor daemon that `-o` stands for. */
export const TOR_PROXY = 'socks5h://127.0.0.1:9050';

/**
 * A proxy entry is pasted into a shell line and written into the agent's TOML
 * config, so only URL characters are accepted: no whitespace, quotes or shell
 * metacharacters. The installer re-checks the same rule when it parses -e.
 */
export const PROXY_URL = /^(socks5h|socks5|socks|http):\/\/[A-Za-z0-9._~%+@:[\]-]+$/;

export function proxyError(entry: string): string {
  const e = entry.trim();
  if (!e) return '';
  if (/^https:\/\//.test(e)) return 'https:// proxies are not supported; use socks5h:// or http://.';
  if (!PROXY_URL.test(e)) {
    return 'A proxy looks like socks5h://host:port or http://host:port, with no spaces or quotes.';
  }
  return '';
}

export interface EgressChoice {
  /** -o: reach the server through the tor daemon on the agent host. */
  tor: boolean;
  /** -e <url>: a proxy tried before Tor, such as a VPN sidecar's. */
  proxy: string;
  /** -e direct: allow the open internet as the last resort. */
  direct: boolean;
}

/**
 * The -e / -o switches in chain order: the proxy first, Tor second, the open
 * internet last and only when asked for. An invalid proxy contributes nothing
 * rather than a broken switch.
 */
export function egressFlags(c: EgressChoice): string {
  const proxy = c.proxy.trim();
  let entry = proxy && !proxyError(proxy) ? proxy : '';
  const on = c.tor || entry !== '';
  // -o already stands for the Tor daemon; naming it again with -e makes the
  // installer refuse the chain as a duplicate.
  if (c.tor && entry === TOR_PROXY) entry = '';
  return [entry && `-e ${entry}`, c.tor && '-o', on && c.direct && '-e direct']
    .filter(Boolean)
    .join(' ');
}

/** True when the proxy field only repeats the Tor daemon that -o adds. */
export function proxyIsTor(c: EgressChoice): boolean {
  return c.tor && c.proxy.trim() === TOR_PROXY;
}

/**
 * The first proxy in the chain, spelled for curl; it fetches the installer
 * itself too. curl resolves the target name locally for socks5:// and speaks
 * SOCKS4 for socks://, so both become socks5h://, which hands the name to the
 * proxy, as the installer does for its own downloads.
 */
export function fetchProxy(c: EgressChoice): string {
  const proxy = c.proxy.trim();
  if (proxy && !proxyError(proxy)) return proxy.replace(/^socks5?:\/\//, 'socks5h://');
  return c.tor ? TOR_PROXY : '';
}

/**
 * Append switches to the EXECUTION line of the pairing service's one-liner,
 * not the download line. The service emits
 *   curl <url> > proxiport-installer.sh
 *   sudo sh proxiport-installer.sh
 * and a similar pair for PowerShell; the switches belong on the second line
 * of each pair, never on the redirect target of the first.
 */
export function appendFlags(base: string, flags: string): string {
  if (!base || !flags) return base;
  const lines = base.split('\n');
  const idx = lines.findIndex(
    (l) => /^\s*(sudo\s+sh|powershell)\b/.test(l) && /proxiport-installer\.(sh|ps1)/.test(l)
  );
  if (idx === -1) return base;
  // Don't double-append: strip trailing switch tokens from a previous pass.
  lines[idx] = lines[idx].replace(/(\s+-[xsrbo]\b|\s+-e\s+\S+)+\s*$/g, '');
  lines[idx] = lines[idx].replace(/\s*$/, '') + ' ' + flags;
  return lines.join('\n');
}

/**
 * Put the installer download on the same proxy the agent will use, so the
 * pairing request stays off the open internet when the agent must.
 */
export function withFetchProxy(base: string, proxy: string): string {
  if (!base || !proxy) return base;
  return base
    .split('\n')
    .map((l) => (/^\s*curl\s+https?:\/\//.test(l) ? l.replace(/^(\s*curl)\s+/, `$1 --proxy ${proxy} `) : l))
    .join('\n');
}
