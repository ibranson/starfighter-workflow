import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    // During `npm run dev`, proxy API calls to a locally-running daemon so the
    // SPA and JSON API share an origin (cookies + same-origin CSRF work).
    // Point this at wherever sfworkflowd is listening.
    proxy: {
      '/api': 'http://127.0.0.1:9090',
      '/healthz': 'http://127.0.0.1:9090'
    }
  }
});
