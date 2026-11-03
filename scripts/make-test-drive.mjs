// Generates test-drive/OfflineLibrary: a tiny sample library for development.
// Safe to re-run; it overwrites only the files it creates.
//
//   practical-manuals  -> a real (generated) PDF: should show "Ready"
//   regional-maps      -> declares a .pmtiles file that isn't there: shows
//                         "Incomplete" and "not supported yet"
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', 'test-drive', 'OfflineLibrary');

function write(rel, data) {
  const p = join(root, rel);
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, data);
  return p;
}
const json = (v) => JSON.stringify(v, null, 2) + '\n';

// ---- Minimal multi-page PDF (ASCII text, Helvetica) ----
function makePdf(pages) {
  const esc = (s) => s.replace(/[\\()]/g, (c) => '\\' + c);
  const objects = [];
  const pageIds = [];
  const fontId = 3 + pages.length * 2;
  pages.forEach((lines, i) => {
    const pageId = 3 + i * 2;
    pageIds.push(pageId);
    const [title, ...body] = lines;
    const stream =
      `BT /F1 22 Tf 72 720 Td (${esc(title)}) Tj ET\n` +
      `BT /F1 12 Tf 16 TL 72 680 Td ` +
      body.map((l) => `(${esc(l)}) Tj T*`).join(' ') +
      ` ET\n`;
    objects[pageId] = `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 ${fontId} 0 R >> >> /Contents ${pageId + 1} 0 R >>`;
    objects[pageId + 1] = `<< /Length ${Buffer.byteLength(stream)} >>\nstream\n${stream}endstream`;
  });
  objects[1] = '<< /Type /Catalog /Pages 2 0 R >>';
  objects[2] = `<< /Type /Pages /Kids [${pageIds.map((id) => `${id} 0 R`).join(' ')}] /Count ${pages.length} >>`;
  objects[fontId] = '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>';

  let out = '%PDF-1.4\n';
  const offsets = [];
  for (let id = 1; id < objects.length; id++) {
    offsets[id] = Buffer.byteLength(out);
    out += `${id} 0 obj\n${objects[id]}\nendobj\n`;
  }
  const xref = Buffer.byteLength(out);
  out += `xref\n0 ${objects.length}\n0000000000 65535 f \n`;
  for (let id = 1; id < objects.length; id++) out += `${String(offsets[id]).padStart(10, '0')} 00000 n \n`;
  out += `trailer\n<< /Size ${objects.length} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(out, 'latin1');
}

const pdf = makePdf([
  [
    'Practical Manuals - Sample Pack',
    'This is placeholder test content for the Offline Library prototype.',
    'It is not a real manual and contains no real instructions.',
    '',
    'If this page opened from the drive with networking turned off,',
    'the launcher, local server and PDF serving all work.',
  ],
  [
    'Page 2',
    'Search test: try finding the word lantern with Ctrl+F (Cmd+F on a Mac).',
    'If the viewer highlights it, in-document search works.',
  ],
]);

write('packs/practical-manuals/content/sample-manual.pdf', pdf);

write('packs/practical-manuals/pack.json', json({
  schemaVersion: 1,
  packId: 'practical-manuals',
  title: 'Practical Manuals (sample)',
  description: 'A generated two-page PDF used to test the PDF viewer.',
  category: 'manuals',
  format: 'pdf',
  version: 'dev',
  languages: ['en'],
  contentDate: '2026-11-03',
  entrypoint: 'content/sample-manual.pdf',
  files: [{ path: 'content/sample-manual.pdf', sizeBytes: pdf.length }],
  source: { name: 'Generated test content', url: '', license: 'Test data, no restrictions', attribution: '' },
}));

write('packs/regional-maps/pack.json', json({
  schemaVersion: 1,
  packId: 'regional-maps',
  title: 'Regional Maps (placeholder)',
  description: 'Declares a map archive that is not included, to test missing-file handling.',
  category: 'maps',
  format: 'pmtiles',
  version: 'dev',
  languages: ['en'],
  contentDate: '2026-11-03',
  entrypoint: 'content/region.pmtiles',
  files: [{ path: 'content/region.pmtiles', sizeBytes: 1048576 }],
  source: { name: 'Placeholder', url: '', license: 'n/a', attribution: '' },
}));

write('library.json', json({
  schemaVersion: 1,
  libraryId: '8c0bb484-b289-4a54-bbf5-39b237f529aa',
  title: 'Test Drive Library',
  packs: ['packs/practical-manuals/pack.json', 'packs/regional-maps/pack.json'],
}));

console.log('Test library written to', root);
