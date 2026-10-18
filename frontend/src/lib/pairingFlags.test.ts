import { describe, it, expect } from 'vitest';
import { appendFlags, egressFlags, fetchProxy, proxyError, withFetchProxy, TOR_PROXY } from './pairingFlags';

const LINUX = 'curl https://pairing.example.net/9L6fHH > proxiport-installer.sh\nsudo sh proxiport-installer.sh';
const WINDOWS =
  '[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12\n' +
  '$url="https://pairing.example.net/9L6fHH"\n' +
  'Invoke-WebRequest -Uri $url -OutFile "proxiport-installer.ps1"\n' +
  'powershell -ExecutionPolicy Bypass -File .\\proxiport-installer.ps1';

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
