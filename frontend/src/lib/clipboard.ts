import { pushToast } from '$lib/stores';

/**
 * Copy text to the clipboard and toast the result. Falls back to a hidden
 * textarea + execCommand for non-secure contexts (e.g. an http:// demo),
 * where navigator.clipboard is unavailable. Resolves to whether the copy
 * worked.
 */
export async function copyToClipboard(text: string, label = 'Copied to clipboard.'): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      // The textarea takes focus to be selected; give it back afterwards so a
      // copy from inside a dialog leaves focus where the user was.
      const prev = document.activeElement as HTMLElement | null;
      (prev?.closest('dialog') ?? document.body).appendChild(ta);
      ta.focus();
      ta.select();
      let ok = false;
      try {
        ok = document.execCommand('copy');
      } finally {
        ta.remove();
        prev?.focus();
      }
      if (!ok) throw new Error('execCommand copy refused');
    }
    pushToast('good', label);
    return true;
  } catch {
    pushToast('bad', 'Copy failed — clipboard unavailable.');
    return false;
  }
}

/**
 * The SSH command to reach an ssh-scheme tunnel. The tunnel forwards
 * serverHost:lport to the remote's SSH port, so a user connects with
 * `ssh -p <lport> <serverHost>` (and supplies their own username).
 */
export function sshCommandFor(host: string, lport?: string): string {
  return `ssh -p ${lport ?? ''} ${host}`;
}
