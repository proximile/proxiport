import { defineConfig } from 'vitest/config';
import path from 'node:path';

// Kept separate from vite.config.ts so the SvelteKit plugin (which needs a
// real .svelte-kit sync and pulls in the whole app graph) stays out of unit
// test runs.
export default defineConfig({
  resolve: {
    alias: {
      '$app/env': path.resolve(import.meta.dirname, 'src/test/stubs/app-env.ts')
    }
  },
  test: {
    // jsdom supplies localStorage, location, btoa and WebSocket, all of which
    // the API client and the token store use.
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    restoreMocks: true,
    clearMocks: true
  }
});
