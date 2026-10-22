/**
 * Svelte action for a native <dialog> rendered inside an {#if}: opens it as a
 * modal (the page behind becomes inert and Tab stays inside), focuses the
 * element marked data-autofocus, and on removal gives focus back to whatever
 * opened it. Escape is left to the caller's oncancel handler.
 */
export function modal(node: HTMLDialogElement, fallback?: () => HTMLElement | null | undefined) {
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  if (!node.open) node.showModal();
  focusInitial(node);
  return {
    destroy() {
      const usable = (el: HTMLElement | null | undefined) =>
        !!el && el.isConnected && !(el as HTMLButtonElement).disabled;
      // Wait a frame: the opener may be re-rendered or disabled by the same
      // state change that closed the dialog.
      requestAnimationFrame(() => {
        if (usable(opener)) opener!.focus();
        else {
          const f = fallback?.();
          if (usable(f)) f!.focus();
        }
      });
    }
  };
}

/** Focus the dialog's data-autofocus element, if it has one. */
export function focusInitial(node: HTMLElement) {
  node.querySelector<HTMLElement>('[data-autofocus]')?.focus();
}
