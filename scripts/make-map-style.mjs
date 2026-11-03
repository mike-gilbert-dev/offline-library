// Writes light and dark MapLibre styles for a Protomaps-basemap pack.
//
//   node scripts/make-map-style.mjs <pack folder> [lang]
//
// URLs in the style start with "{base}", which the app replaces with the
// pack's local address at runtime. The style never refers to the internet,
// and the attribution is plain text, not a link.
import { mkdirSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(join(here, '..', 'web', 'package.json'));
const { layers, namedFlavor } = require('@protomaps/basemaps');

const packDir = process.argv[2];
const lang = process.argv[3] ?? 'en';
if (!packDir) {
  console.error('usage: node scripts/make-map-style.mjs <pack folder> [lang]');
  process.exit(1);
}

for (const flavor of ['light', 'dark']) {
  const style = {
    version: 8,
    name: `Protomaps ${flavor}`,
    glyphs: '{base}assets/fonts/{fontstack}/{range}.pbf',
    sprite: `{base}assets/sprites/${flavor}`,
    sources: {
      protomaps: {
        type: 'vector',
        url: 'pmtiles://{base}content/region.pmtiles',
        attribution: '© OpenStreetMap contributors · Protomaps',
      },
    },
    layers: layers('protomaps', namedFlavor(flavor), { lang }),
  };
  const out = join(packDir, 'assets', `style-${flavor}.json`);
  mkdirSync(dirname(out), { recursive: true });
  writeFileSync(out, JSON.stringify(style));
  console.log('wrote', out, `(${style.layers.length} layers)`);
}
