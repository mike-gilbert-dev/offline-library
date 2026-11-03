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
| `server/zim/` | Pure-Go ZIM reader. It supports format v5 and v6, old and new namespaces, xz and zstd clusters, redirects, and title-prefix search. It reads with random access, so archives are never loaded into memory. |
| `server/zimserve.go` | Serves ZIM content from a **second origin** (a separate port) so pack scripts can't reach the app API. Also serves the ZIM search and info API. |
| `scripts/fetch-sample-zim.ps1` | Downloads the sample Wikipedia ZIM (~51 MB) and checks its SHA-256. The file is too large for git, so it's not committed. |
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

## ZIM (Wikipedia) packs

- Run `scripts\fetch-sample-zim.ps1` once to add the sample Wikipedia pack to `test-drive`.
- Packs open in an in-app viewer (`#/pack/<id>/<article>`). The article is shown in a
  sandboxed iframe served from the content origin. Its CSP allows only the
  archive's own scripts and assets and forbids every network request
  (`connect-src 'self'`), and only the app may frame it.
- A small injected script (`/__olib/bridge.js`) reports the current page
  to the app and intercepts external links. The app asks the user before
  opening them in the browser.
- Search is **title-prefix search** (fast, using the archive's title index). Full-text
  search would need the archive's embedded Xapian index, which has no Go
  implementation.

## Map (PMTiles) packs

- Run `scripts\fetch-sample-map.ps1` once to build the sample map pack (Washington, D.C.,
  zoom 0–15, about 56 MB). It needs `tools\pmtiles\pmtiles.exe` from the
  [go-pmtiles releases](https://github.com/protomaps/go-pmtiles/releases).
  Pass `-Bbox minLon,minLat,maxLon,maxLat` for another region.
- A map pack contains the `.pmtiles` archive plus its fonts, icons, and light and dark
  MapLibre styles (`assets/style-*.json`). Styles refer to pack files as `{base}…`.
  They contain no internet URLs, and the attribution is plain text.
- The viewer is MapLibre GL 6 with the pmtiles protocol. It reads tiles over HTTP range requests
  from `/content/`. It is lazy-loaded, so the library page stays small.
- Maps support browsing and zooming only. Place search and routing are separate features.
- Headless Edge with SwiftShader draws only the map background, so check maps in a real browser.

## Status

Phase 0: the launcher, manifest validation, pack catalog, PDFs, ZIM, and map spikes
all work on Windows. Next: a test on macOS.
