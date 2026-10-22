import { describe, it, expect } from 'vitest';
import {
  appendFlags,
  egressFlags,
  fetchProxy,
  installerFlags,
  proxyError,
  proxyIsTor,
  withFetchProxy,
  TOR_PROXY
} from './pairingFlags';

const LINUX = 'curl https://pairing.example.net/9L6fHH > proxiport-installer.sh\nsudo sh proxiport-installer.sh';
const WINDOWS =
  '[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12\n' +
  '$url="https://pairing.example.net/9L6fHH"\n' +
  'Invoke-WebRequest -Uri $url -OutFile "proxiport-installer.ps1"\n' +
  'powershell -ExecutionPolicy Bypass -File .\\proxiport-installer.ps1';

describe('installerFlags', () => {
  const all = { x: true, s: true, r: true, b: true };

  it('emits every switch that is on', () => {
    expect(installerFlags(all)).toBe('-x -s -r -b');
    expect(installerFlags({ x: false, s: false, r: false, b: false })).toBe('');
  });

  // A sudo switch grants passwordless sudo on its own in the installer, so it
  // must never be sent without the option it extends.
  it('drops -s without -x and -b without -r', () => {
    expect(installerFlags({ ...all, x: false })).toBe('-r -b');
    expect(installerFlags({ ...all, r: false })).toBe('-x -s');
    expect(installerFlags({ ...all, x: false, r: false })).toBe('');
  });

  it('gives the PowerShell installer only the switches it declares', () => {
    expect(installerFlags(all, 'windows')).toBe('-x -r');
    expect(installerFlags({ ...all, x: false }, 'windows')).toBe('-r');
  });
});

describe('egressFlags', () => {
  it('is empty with nothing chosen', () => {
    expect(egressFlags({ tor: false, proxy: '', direct: false })).toBe('');
  });

  it('keeps chain order: proxy, then Tor, then the open internet', () => {
    expect(egressFlags({ tor: true, proxy: 'socks5h://gluetun:1080', direct: true })).toBe(
      '-e socks5h://gluetun:1080 -o -e direct'
    );
  });

  // The fallback is only a fallback: with no proxy ahead of it, "-e direct"
  // alone would write a one-entry chain that changes nothing.
  it('ignores the fallback when there is nothing to fall back from', () => {
    expect(egressFlags({ tor: false, proxy: '', direct: true })).toBe('');
  });

  it('drops an invalid proxy instead of emitting a broken switch', () => {
    expect(egressFlags({ tor: true, proxy: 'socks5h://a b', direct: false })).toBe('-o');
    expect(egressFlags({ tor: false, proxy: 'socks5h://a"b', direct: true })).toBe('');
  });

  // The installer refuses a chain that names the same entry twice.
  it('lists the Tor daemon once when the proxy field repeats it', () => {
    expect(egressFlags({ tor: true, proxy: TOR_PROXY, direct: false })).toBe('-o');
    expect(egressFlags({ tor: true, proxy: ` ${TOR_PROXY} `, direct: true })).toBe('-o -e direct');
    expect(egressFlags({ tor: false, proxy: TOR_PROXY, direct: false })).toBe(`-e ${TOR_PROXY}`);
    expect(proxyIsTor({ tor: true, proxy: TOR_PROXY, direct: false })).toBe(true);
    expect(proxyIsTor({ tor: false, proxy: TOR_PROXY, direct: false })).toBe(false);
    expect(proxyIsTor({ tor: true, proxy: 'socks5h://gluetun:1080', direct: false })).toBe(false);
  });

  it('trims the proxy', () => {
    expect(egressFlags({ tor: false, proxy: '  http://proxy.example:3128 ', direct: false })).toBe(
      '-e http://proxy.example:3128'
    );
  });
});

describe('proxyError', () => {
  it.each(['socks5h://127.0.0.1:9050', 'socks5://h:1', 'socks://h:1', 'http://user:pw@proxy.example:3128', 'socks5h://[::1]:1080'])(
    'accepts %s',
    (s) => expect(proxyError(s)).toBe('')
  );
  // Everything here would either break the shell line, break the TOML the
  // installer writes, or be refused by the agent at startup.
  it.each(['https://h:1', 'ftp://h:1', 'tor', 'socks5h://h:1 direct', "socks5h://h'1", 'socks5h://h;rm', 'socks5h://h$x'])(
    'refuses %s',
    (s) => expect(proxyError(s)).not.toBe('')
  );
  it('says nothing about an empty field', () => {
    expect(proxyError('')).toBe('');
    expect(proxyError('   ')).toBe('');
  });
});

describe('fetchProxy', () => {
  it('is the first proxy in the chain', () => {
    expect(fetchProxy({ tor: true, proxy: '', direct: false })).toBe(TOR_PROXY);
    expect(fetchProxy({ tor: true, proxy: 'socks5h://gluetun:1080', direct: false })).toBe('socks5h://gluetun:1080');
    expect(fetchProxy({ tor: false, proxy: '', direct: true })).toBe('');
    expect(fetchProxy({ tor: false, proxy: 'http://proxy.example:3128', direct: false })).toBe('http://proxy.example:3128');
  });

  // curl resolves the name itself for socks5:// and speaks SOCKS4 for
  // socks://; socks5h:// sends the name to the proxy instead.
  it('spells SOCKS for curl as socks5h://', () => {
    expect(fetchProxy({ tor: false, proxy: 'socks5://gluetun:1080', direct: false })).toBe('socks5h://gluetun:1080');
    expect(fetchProxy({ tor: false, proxy: 'socks://gluetun:1080', direct: false })).toBe('socks5h://gluetun:1080');
    expect(fetchProxy({ tor: true, proxy: 'socks5h://gluetun:1080', direct: false })).toBe('socks5h://gluetun:1080');
  });

  it('changes only the download line, not the entry given to the installer', () => {
    const c = { tor: false, proxy: 'socks5://gluetun:1080', direct: false };
    expect(withFetchProxy(appendFlags(LINUX, egressFlags(c)), fetchProxy(c))).toBe(
      'curl --proxy socks5h://gluetun:1080 https://pairing.example.net/9L6fHH > proxiport-installer.sh\n' +
        'sudo sh proxiport-installer.sh -e socks5://gluetun:1080'
    );
  });
});

describe('appendFlags', () => {
  it('puts switches on the execution line only', () => {
    expect(appendFlags(LINUX, '-x -o')).toBe(
      'curl https://pairing.example.net/9L6fHH > proxiport-installer.sh\nsudo sh proxiport-installer.sh -x -o'
    );
    expect(appendFlags(WINDOWS, '-x').split('\n')[3]).toBe('powershell -ExecutionPolicy Bypass -File .\\proxiport-installer.ps1 -x');
  });

  it('does not stack switches over a previous pass, including -e with an argument', () => {
    const once = appendFlags(LINUX, '-x -e socks5h://gluetun:1080 -o -e direct');
    expect(appendFlags(once, '-r')).toBe(
      'curl https://pairing.example.net/9L6fHH > proxiport-installer.sh\nsudo sh proxiport-installer.sh -r'
    );
  });

  it('leaves the text alone when there is nothing to add or nowhere to add it', () => {
    expect(appendFlags(LINUX, '')).toBe(LINUX);
    expect(appendFlags('', '-x')).toBe('');
    expect(appendFlags('echo hello', '-x')).toBe('echo hello');
  });
});

describe('withFetchProxy', () => {
  it('routes the download through the proxy and leaves the execution line alone', () => {
    expect(withFetchProxy(LINUX, TOR_PROXY)).toBe(
      `curl --proxy ${TOR_PROXY} https://pairing.example.net/9L6fHH > proxiport-installer.sh\nsudo sh proxiport-installer.sh`
    );
  });
  it('is a no-op without a proxy', () => {
    expect(withFetchProxy(LINUX, '')).toBe(LINUX);
  });
});
