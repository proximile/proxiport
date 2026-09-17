// Stub for SvelteKit's virtual $app/environment module under vitest.
//
// The real module is provided by the SvelteKit vite plugin at dev/build time
// and does not exist as a resolvable file, so unit tests get this instead.
// browser=true is deliberate: it is the branch the SPA actually runs in, and
// it lets the token store's localStorage persistence be tested.
export const browser = true;
export const dev = false;
export const building = false;
export const version = 'test';
