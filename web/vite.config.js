import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The production build is embedded into the Go launcher (server/webdist).
// In development, run the launcher with -dev (port 8787) and proxy to it.
export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../server/webdist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8787',
      '/content': 'http://127.0.0.1:8787',
    },
  },
});
