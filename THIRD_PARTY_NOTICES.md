# Third-party notices

Offline Library is licensed under the GNU General Public License v3.0 (see
`LICENSE`). It includes the following third-party software and assets, each
under its own license. All of them are compatible with GPL-3.0.

## Built into the launcher (Go)

| Component | License | Source |
| --- | --- | --- |
| klauspost/compress (zstd) | BSD-3-Clause | https://github.com/klauspost/compress |
| ulikunitz/xz | BSD-3-Clause | https://github.com/ulikunitz/xz |
| Go standard library | BSD-3-Clause | https://go.dev |

## Built into the web interface

| Component | License | Source |
| --- | --- | --- |
| @meshtastic/core, @meshtastic/protobufs | GPL-3.0 | https://github.com/meshtastic/web, https://github.com/meshtastic/protobufs |
| @bufbuild/protobuf | Apache-2.0 and BSD-3-Clause | https://github.com/bufbuild/protobuf-es |
| tslog | MIT | https://github.com/fullstack-build/tslog |
| ste-simple-events | MIT | https://github.com/KeesCBakker/Strongly-Typed-Events-for-TypeScript |
| crc | MIT | https://github.com/alexgorbatchev/crc |
| MapLibre GL JS | BSD-3-Clause | https://github.com/maplibre/maplibre-gl-js |
| PMTiles | BSD-3-Clause | https://github.com/protomaps/PMTiles |
| Protomaps basemaps (map styles) | BSD-3-Clause | https://github.com/protomaps/basemaps |
| Svelte | MIT | https://github.com/sveltejs/svelte |
| shadcn-svelte components (copied into `web/src/lib/components/ui`) | MIT | https://github.com/huntabyte/shadcn-svelte |
| Tailwind CSS, tw-animate-css, tailwind-variants, clsx, cn | MIT | https://tailwindcss.com and respective repositories |
| Lucide icons (@lucide/svelte) | ISC | https://github.com/lucide-icons/lucide |
| Geist font (@fontsource-variable/geist) | SIL Open Font License 1.1 | https://github.com/vercel/geist-font |

Each package's full license text ships with it (for example in
`web/node_modules/<package>/LICENSE` after `npm install`, or in the Go module
cache).

## Content in packs

Packs are data, not part of this program, and carry their own licenses. The
sample-pack scripts download or build:

- **Wikipedia (ZIM) via Kiwix:** text under CC BY-SA 4.0; media under their
  individual licenses. https://www.kiwix.org
- **Maps (PMTiles) from the Protomaps planet build:** map data © OpenStreetMap
  contributors, ODbL 1.0 (https://www.openstreetmap.org/copyright). Map fonts
  are under the SIL Open Font License; sprites under their stated licenses in
  https://github.com/protomaps/basemaps-assets.
