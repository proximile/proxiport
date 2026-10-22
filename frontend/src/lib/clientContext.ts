/**
 * The client record loaded by the /inventory/[id] layout, shared with its tab
 * pages so they do not each fetch GET /clients/:id again.
 */
import { getContext, setContext } from 'svelte';
import type { Client } from './types';

export type ClientContext = {
  /** The record for the current route id, or null while loading or after a failure. */
  readonly client: Client | null;
  readonly loading: boolean;
  readonly error: string;
  /** Re-fetch the current client (for example after creating a tunnel). */
  reload: () => Promise<void>;
};

const KEY = Symbol('inventory-client');

export function setClientContext(ctx: ClientContext): void {
  setContext(KEY, ctx);
}

export function getClientContext(): ClientContext | undefined {
  return getContext<ClientContext | undefined>(KEY);
}
