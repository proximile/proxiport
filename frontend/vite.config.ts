import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// API proxy target for `npm run dev`. Override per-developer via env, e.g.:
//   PROXIPORT_DEV_API=https://my-proxiport.example.com npm run dev
// Defaults to a local proxiportd on the standard port.
const apiTarget = process.env.PROXIPORT_DEV_API ?? 'http://127.0.0.1:8000';

export default defineConfig({
  plugins: [tailwindcss(), sveltekit()],
  server: {
    port: 5173,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
        secure: true
      }
    }
  }
});
