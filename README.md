# Offline Library

A portable, offline knowledge library that runs from an external drive.
A small launcher on the drive starts a local web server bound to `127.0.0.1`
and opens the computer's own browser. Nothing is installed and nothing is
fetched from the internet.

```text
DRIVE/
  start-windows.exe          ← double-click on Windows
  start-mac-apple-silicon    ← M1/M2/M3… Macs
  start-mac-intel
  start-linux
  OfflineLibrary/
    library.json             ← lists the packs (authoritative)
    packs/<pack-id>/pack.json
    packs/<pack-id>/content/…
```

## Layout

| Path | What |
| --- | --- |
| `server/` | Go launcher and server: library scan and validation, token auth, content serving. The built UI is embedded from `server/webdist`. |
| `web/` | Svelte 5 + TypeScript + Vite UI, styled with Tailwind v4 and [shadcn-svelte](https://www.shadcn-svelte.com) (the "vega" style with the neutral base color). The Geist font is bundled locally, so the UI makes no network requests. |
| `web/src/lib/components/ui/` | shadcn-svelte components. These are copied source files that we own and can edit. Add more with `npx shadcn-svelte@latest add <name> -y`. |
| `scripts/make-test-drive.mjs` | Generates `test-drive/OfflineLibrary` (sample PDF pack plus a deliberately incomplete maps pack). |
| `scripts/build.ps1` | Builds the UI, cross-compiles all four launchers, and assembles `dist/drive`. |

## Build

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
```

Copy everything in `dist\drive` to the root of the exFAT drive.

## Develop

```powershell
# terminal 1: API server against the test library (no token, port 8787)
cd server; go run . -dev -library ..\test-drive\OfflineLibrary

# terminal 2: UI with hot reload
cd web; npm run dev
```

Tests: `cd server; go test ./...`

## Launcher flags

`-library <path>` uses a specific library folder. `-port <n>` uses a fixed port.
`-no-browser` doesn't open a browser. `-dev` is development mode.

## Pack statuses

| Status | Meaning |
| --- | --- |
| `ok` | Valid; the app can open it. |
| `unsupported` | Valid, but this build can't open its `format` yet. |
| `incomplete` | Files are missing or the wrong size (partial copy, or the drive was unplugged). |
| `invalid` | The manifest is malformed, unsafe (path escapes), or uses a duplicate pack ID. |

## Security model

- The server listens on loopback only and on a random port. The launcher
  opens `/auth?token=…`, which sets an HttpOnly, SameSite=Strict cookie that
  every request must carry.
- The Host header must match `127.0.0.1:<port>`, which blocks DNS rebinding.
  POSTs with a foreign `Origin` are rejected.
- `/content/` serves only files declared in a pack manifest and verified by
  the last scan. Paths can't contain `..`, `\` or `:`, or be absolute, and
  symlinks may not escape the pack folder.
- The library is opened read-only. Nothing is written to the drive.

## Status

Phase 0 / step 1–2 of the plan: launcher, manifest validation, pack catalog,
and PDFs opening in the browser's built-in viewer. Next: test from the
exFAT drive with networking off, then the ZIM and map spikes.
