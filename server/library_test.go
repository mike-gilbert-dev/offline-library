package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoinRejectsEscapes(t *testing.T) {
	base := t.TempDir()
	bad := []string{
		"", "/etc/passwd", "../x", "a/../../x", "a/..", "content/../../secret",
		"C:/Windows/win.ini", "C:x", `content\a.pdf`, `\\server\share\x`,
		"a//b", "a/", "a\x00b",
	}
	for _, rel := range bad {
		if _, err := SafeJoin(base, rel); err == nil {
			t.Errorf("SafeJoin(%q) should have been rejected", rel)
		}
	}
}

func TestSafeJoinAccepts(t *testing.T) {
	base := t.TempDir()
	for _, rel := range []string{"pack.json", "content/a.pdf", "./content/a.pdf", "content/sub dir/Ã¼nÃ¯code.pdf"} {
		got, err := SafeJoin(base, rel)
		if err != nil {
			t.Errorf("SafeJoin(%q) unexpected error: %v", rel, err)
			continue
		}
		if !within(base, got) {
			t.Errorf("SafeJoin(%q) = %q, outside base", rel, got)
		}
	}
}

func TestSafeJoinRejectsSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	write(t, secret, "secret")
	base := t.TempDir()
	if err := os.Symlink(secret, filepath.Join(base, "link.txt")); err != nil {
		t.Skipf("symlinks not available here: %v", err)
	}
	if _, err := SafeJoin(base, "link.txt"); err == nil {
		t.Fatal("symlink pointing outside base should be rejected")
	}
}

func TestScanMissingLibrary(t *testing.T) {
	lib := Scan(filepath.Join(t.TempDir(), "nope"))
	if lib.Available || len(lib.Problems) == 0 {
		t.Fatalf("expected unavailable library with a problem, got %+v", lib)
	}
}

func TestScanUnsupportedSchema(t *testing.T) {
	root := t.TempDir()
	writeJSONFile(t, filepath.Join(root, "library.json"), map[string]any{"schemaVersion": 2, "libraryId": "x", "packs": []string{}})
	lib := Scan(root)
	if !lib.Available || len(lib.Problems) == 0 {
		t.Fatalf("expected a schema problem, got %+v", lib)
	}
}

func TestScanPackStatuses(t *testing.T) {
	root := t.TempDir()
	pdf := "%PDF-1.4 fake"

	// ok: valid pdf pack
	writePack(t, root, "good", "good", "pdf", "content/a.pdf", int64(len(pdf)))
	write(t, filepath.Join(root, "packs/good/content/a.pdf"), pdf)
	// incomplete: declared file absent
	writePack(t, root, "missing", "missing", "pdf", "content/a.pdf", 100)
	// incomplete: wrong size (partial copy)
	writePack(t, root, "short", "short", "pdf", "content/a.pdf", 999)
	write(t, filepath.Join(root, "packs/short/content/a.pdf"), pdf)
	// unsupported: valid but unknown format
	writePack(t, root, "maps", "maps", "epub", "content/r.epub", int64(len(pdf)))
	write(t, filepath.Join(root, "packs/maps/content/r.epub"), pdf)
	// invalid: path traversal
	writePack(t, root, "evil", "evil", "pdf", "../good/content/a.pdf", int64(len(pdf)))
	// invalid: duplicate id
	writePack(t, root, "dupe", "good", "pdf", "content/a.pdf", int64(len(pdf)))
	write(t, filepath.Join(root, "packs/dupe/content/a.pdf"), pdf)
	// invalid: broken JSON
	write(t, filepath.Join(root, "packs/broken/pack.json"), "{not json")

	writeJSONFile(t, filepath.Join(root, "library.json"), map[string]any{
		"schemaVersion": 1, "libraryId": "test", "title": "Test",
		"packs": []string{
			"packs/good/pack.json", "packs/missing/pack.json", "packs/short/pack.json",
			"packs/maps/pack.json", "packs/evil/pack.json", "packs/dupe/pack.json",
			"packs/broken/pack.json", "packs/nowhere/pack.json", "../outside/pack.json",
		},
	})

	lib := Scan(root)
	want := []PackStatus{
		StatusOK, StatusIncomplete, StatusIncomplete, StatusUnsupported,
		StatusInvalid, StatusInvalid, StatusInvalid, StatusIncomplete, StatusInvalid,
	}
	if len(lib.Packs) != len(want) {
		t.Fatalf("got %d packs, want %d", len(lib.Packs), len(want))
	}
	for i, p := range lib.Packs {
		if p.Status != want[i] {
			t.Errorf("%s: status %q, want %q (problems: %v)", p.ManifestPath, p.Status, want[i], p.Problems)
		}
		if p.Status == StatusOK && len(p.files) == 0 {
			t.Errorf("%s: ok pack has no servable files", p.ManifestPath)
		}
		if p.Status != StatusOK && p.files != nil {
			t.Errorf("%s: non-ok pack must not expose files", p.ManifestPath)
		}
	}
}

func writePack(t *testing.T, root, dir, id, format, file string, size int64) {
	t.Helper()
	writeJSONFile(t, filepath.Join(root, "packs", dir, "pack.json"), map[string]any{
		"schemaVersion": 1, "packId": id, "title": id, "format": format,
		"entrypoint": file, "files": []map[string]any{{"path": file, "sizeBytes": size}},
	})
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(b))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
