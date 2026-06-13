import { pushToast } from '$lib/stores';

/**
 * Copy text to the clipboard and toast the result. Falls back to a hidden
 * textarea + execCommand for non-secure contexts (e.g. an http:// demo),
 * where navigator.clipboard is unavailable.
 */
export async function copyToClipboard(text: string, label = 'Copied to clipboard.'): Promise<void> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.focus();
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
    pushToast('good', label);
  } catch {
    pushToast('bad', 'Copy failed — clipboard unavailable.');
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
