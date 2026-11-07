// Browser stand-ins for the Node built-ins (os, path, util) that the logger
// bundled inside @meshtastic/core imports. Its browser code path doesn't call
// them; these exist so the build resolves cleanly and nothing throws if a
// Node-only branch is ever reached.
export const hostname = undefined;
export const normalize = (p: string) => p;
export const formatWithOptions = (_options: unknown, ...args: unknown[]) => args.map(String).join(' ');
export const types = { isError: (e: unknown) => e instanceof Error };
export default { hostname, normalize, formatWithOptions, types };
