import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  preprocess: vitePreprocess(),
  kit: {
    // adapter-static emits a directory of files we copy into the Go binary's
    // embed dir (internal/web/dist) via `make web`.
    adapter: adapter({
      pages: 'build',
      assets: 'build',
      fallback: 'index.html',
      precompress: false,
      strict: true
    }),
    // SPA mode: single HTML entry, client-side routing for everything else.
    prerender: { entries: [] }
  }
};

export default config;
