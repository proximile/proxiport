/**
 * Waiting for the result of a file push.
 *
 * POST /files answers as soon as the server has staged the file; the transfer
 * to the client happens afterwards, and its outcome is only reported on the
 * /ws/uploads socket as one message per target client:
 * {client_id, uuid, filepath, size, status: "success" | "ignored" | "error", message}.
 */

export type UploadOutcome = {
  client_id: string;
  uuid: string;
  filepath?: string;
  size?: number;
  status: string;
  message?: string;
};

/** The part of a WebSocket the watcher uses. */
export type MessageSource = {
  readyState: number;
  addEventListener(type: string, listener: (ev: any) => void): void;
  removeEventListener(type: string, listener: (ev: any) => void): void;
  close(): void;
};

const WS_OPEN = 1;

/** Resolve true once the socket is open, false if it fails or takes too long. */
export function waitForOpen(sock: MessageSource, timeoutMs = 5000): Promise<boolean> {
  if (sock.readyState === WS_OPEN) return Promise.resolve(true);
  return new Promise((resolve) => {
    const done = (ok: boolean) => {
      clearTimeout(timer);
      sock.removeEventListener('open', onOpen);
      sock.removeEventListener('error', onFail);
      sock.removeEventListener('close', onFail);
      resolve(ok);
    };
    const onOpen = () => done(true);
    const onFail = () => done(false);
    const timer = setTimeout(() => done(false), timeoutMs);
    sock.addEventListener('open', onOpen);
    sock.addEventListener('error', onFail);
    sock.addEventListener('close', onFail);
  });
}

/**
 * Collect upload results from a socket. Messages are buffered from the moment
 * the watcher is created, so a result that arrives before the POST response
 * (which carries the uuid to wait for) is not lost.
 */
export function watchUploads(sock: MessageSource) {
  const seen: UploadOutcome[] = [];
  let waiter: ((o: UploadOutcome) => boolean) | null = null;

  const onMessage = (ev: { data: unknown }) => {
    let msg: UploadOutcome;
    try {
      msg = JSON.parse(String(ev.data));
    } catch {
      return;
    }
    if (!msg || typeof msg !== 'object' || !msg.uuid) return;
    if (waiter && waiter(msg)) return;
    seen.push(msg);
  };
  sock.addEventListener('message', onMessage);

  return {
    /**
     * Resolve with the result for this upload and client, or null when none
     * arrives within timeoutMs, the socket closes first, or the signal aborts.
     */
    wait(uuid: string, clientId: string, timeoutMs: number, signal?: AbortSignal): Promise<UploadOutcome | null> {
      const matches = (o: UploadOutcome) => o.uuid === uuid && o.client_id === clientId;
      const early = seen.find(matches);
      if (early) return Promise.resolve(early);
      return new Promise((resolve) => {
        const finish = (o: UploadOutcome | null) => {
          clearTimeout(timer);
          waiter = null;
          sock.removeEventListener('close', onClose);
          signal?.removeEventListener('abort', onClose);
          resolve(o);
        };
        const onClose = () => finish(null);
        const timer = setTimeout(() => finish(null), timeoutMs);
        waiter = (o) => {
          if (!matches(o)) return false;
          finish(o);
          return true;
        };
        sock.addEventListener('close', onClose);
        if (signal?.aborted) finish(null);
        else signal?.addEventListener('abort', onClose);
      });
    },
    close() {
      sock.removeEventListener('message', onMessage);
      try {
        sock.close();
      } catch {
        /* already closed */
      }
    }
  };
}

/**
 * The path the file will be written to. A path ending in a separator names a
 * folder, so the local file's name is appended; an empty field means /tmp/.
 */
export function resolveRemotePath(dest: string, fileName: string): string {
  const d = dest.trim();
  if (!d) return `/tmp/${fileName}`;
  if (d.endsWith('/') || d.endsWith('\\')) return `${d}${fileName}`;
  return d;
}
