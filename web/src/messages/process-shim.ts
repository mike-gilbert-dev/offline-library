// The npm build of @meshtastic/core bundles tslog's Node.js runtime, which
// reads the Node global `process` while the module loads. Browsers have no
// `process`, so define a minimal one before the library is imported.
// mesh.svelte.ts removes it again once the library has loaded.
const g = globalThis as { process?: unknown };
export const installedProcessShim = typeof g.process === 'undefined';
if (installedProcessShim) g.process = { version: '', env: {}, cwd: () => '' };
