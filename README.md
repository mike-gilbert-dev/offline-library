# Offline Library

A portable, offline knowledge library that runs from an external drive.
A small launcher on the drive starts a local web server bound to `127.0.0.1`
and opens the computer's own browser. Nothing is installed, and the library
never goes online on its own: the only network feature is an update check
that runs when the user presses **Check for updates**.

It can also send and receive [Meshtastic](https://meshtastic.org) messages
through a LoRa radio plugged in over USB, so it stays useful without internet
or cell service.

```text
DRIVE/
  start-windows.exe          ← double-click on Windows
  start-mac-apple-silicon    ← M1/M2/M3… Macs
  start-mac-intel
  start-linux
  Start Offline Library.command  ← double-click helper for macOS
  OfflineLibrary/
    library.json             ← lists the packs (authoritative)
    packs/<pack-id>/pack.json
    packs/<pack-id>/content/…
    data/messages/<node>.jsonl ← Meshtastic message history (created on use)
```

Updates replace only the `start-*` launchers; `OfflineLibrary/` is never
touched by the updater.

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
| `web/src/messages/` | Meshtastic Messages: radio connection over Web Serial, conversations, delivery status, history. Uses `@meshtastic/core` with our own serial transport and browser shims (see the comments in those files). |
| `server/messages.go` | Message history API: append-only JSON Lines per radio in `OfflineLibrary/data/messages/`. |
| `server/update*.go`, `server/releasekey.go`, `server/restart_*.go` | In-app updates: release check, signed-manifest verification, download, file swap and restart. |
| `server/cmd/releasesign/` | Creates the release signing key and signs `release.json`. |
| `scripts/build.ps1` | Builds the UI, cross-compiles all four launchers, and assembles `dist/drive`. `-Version v1.2.3` makes a release build. |
| `scripts/release.ps1` | Builds, signs and zips a release; with `-Publish`, tags, pushes and creates the GitHub release. |

## Build

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
```

For a new drive, copy everything in `dist\drive` to the root of the exFAT
drive. To update a drive that already has packs, copy only the `start-*` files
and `Start Offline Library.command` (the `OfflineLibrary` folder in `dist\drive`
is a small test library). Requires Go and Node.js; the built launchers and UI
aren't stored in git.

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
- Packs are opened read-only. The only thing written to the library folder is
  Meshtastic message history (`OfflineLibrary/data/messages/`), through an API
  that accepts only an 8-hex-digit node ID as the file name.
- The update check and install are the only outbound requests, and run only
  when the user asks. Updates must carry an Ed25519 signature from the
  release key (public key in `server/releasekey.go`); files are checked
  against the signed checksums before anything is replaced. No credentials
  are built into the launcher.

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

## Finding packs: search, tags and pages

- The library page has a search box, tag filters and pages (12, 24, 48 or 96 packs per page).
- Search matches every typed word against a pack's title, description, category, format, source and tags.
- Selecting several tags shows only packs that have all of them. The **untagged** filter helps you find packs you haven't sorted yet.
- Tags belong to the user. Add or remove them with **Add tags**/**Edit tags** on a pack's card; rename, merge or delete them in **Settings → Tags**.
- Tags are lowercased, can be up to 40 characters, and a pack can have up to 30 of them.
- They're stored in `OfflineLibrary/data/tags.json`, keyed by `packId`, never in `pack.json`. Replacing a pack keeps its tags, and tags for packs that aren't currently listed are kept.
- The file is rewritten atomically (temp file + rename). If it gets damaged, the app refuses to overwrite it and says so.

## Messages (Meshtastic)

- Built into the app (the **Messages** button), not a pack. It needs Chrome,
  Edge or Brave, because it uses Web Serial to reach the radio over USB.
- Shows channels and direct-message threads with delivery status
  (delivered, relayed, failed) taken from the radio's routing receipts.
  Sending on the public channel is turned off for now.
- History is saved per radio on the drive and loaded when that radio is
  connected again, on any computer.
- Tested with Heltec Wireless Tracker radios (ESP32-S3) on firmware 2.6 and
  2.6 and 2.7. With 2.7, the radio's USB port reboots it into download mode
  on the esptool DTR/RTS sequence (RTS on with DTR off, then DTR on with RTS
  off, then both off), so tools that toggle those lines can trigger it.
  Connecting from the app on 2.7 still needs a full connect/disconnect test.

## Updates and releases

- **Settings → Check for updates** asks GitHub for the latest release;
  **Install** downloads the launchers for all platforms (so the drive keeps
  working everywhere), verifies them, swaps them in by renaming (keeping
  `.old` copies until the new version starts), and restarts.
- To publish: commit everything on `main`, then
  `scripts\release.ps1 -Version v1.2.3 -Notes notes.md -Publish`. Without
  `-Publish` it only builds and signs, for checking.
- Releases are signed with a private key kept outside the repository
  (`%USERPROFILE%\.offline-library\release-signing.key` by default). Without
  that key, no update can be published that existing installs will accept.
- `OLIB_GITHUB_TOKEN` lets the launcher read releases of a private repository;
  `OLIB_UPDATE_API` points it at another API server for testing (signatures
  are still required).

## License

GPL-3.0; see `LICENSE`. Third-party components and their licenses are listed
in `THIRD_PARTY_NOTICES.md`. To report a security problem, see `SECURITY.md`.

## Status

The launcher, pack catalog, PDFs, ZIM, maps, Meshtastic Messages and in-app
updates work on Windows; the library, PDFs, ZIM and maps are also verified on
macOS. Not yet tested: updating on macOS and on an exFAT drive, and Linux.
