import path from 'node:path';
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

// The production build is embedded into the Go launcher (server/webdist).
// In development, run the launcher with -dev (port 8787) and proxy to it.
export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  resolve: {
    alias: [
      { find: '$lib', replacement: path.resolve('./src/lib') },
      // @meshtastic/core bundles a logger that imports Node built-ins.
      { find: /^(os|path|util)$/, replacement: path.resolve('./src/messages/node-shim.ts') },
    ],
  },
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
