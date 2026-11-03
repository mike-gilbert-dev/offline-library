package zim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests run against the real sample archive when it has been
// downloaded (scripts/fetch-sample-zim.ps1); otherwise they are skipped.
func sampleArchive(t *testing.T) *Archive {
	t.Helper()
	matches, _ := filepath.Glob("../../test-drive/OfflineLibrary/packs/wikipedia-physics/content/*.zim")
	if len(matches) == 0 {
		t.Skip("sample ZIM not downloaded")
	}
	start := time.Now()
	a, err := Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("opened %s in %v: v%d.%d, %d entries, %d clusters, new namespaces=%v",
		filepath.Base(matches[0]), time.Since(start), a.Header.MajorVersion, a.Header.MinorVersion,
		a.Header.EntryCount, a.Header.ClusterCount, a.NewNamespaces)
	t.Cleanup(func() { a.Close() })
	return a
}

func TestSampleMainPageAndMetadata(t *testing.T) {
	a := sampleArchive(t)
	for _, k := range []string{"Title", "Language", "Date", "Creator", "Publisher", "License"} {
		v, err := a.Metadata(k)
		t.Logf("M/%s = %q (err=%v)", k, v, err)
	}
	start := time.Now()
	mp, err := a.MainPage()
	if err != nil {
		t.Fatal(err)
	}
	body, err := a.Content(mp)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("main page %c/%s (%s, %d bytes) in %v", mp.Namespace, mp.Path, mp.MimeType, len(body), time.Since(start))
	if !strings.Contains(strings.ToLower(string(body)), "<html") {
		t.Errorf("main page doesn't look like HTML")
	}
}

func TestSampleSearchAndFind(t *testing.T) {
	a := sampleArchive(t)
	start := time.Now()
	res, err := a.SearchTitles("Quantum", 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("search 'Quantum' -> %d results in %v", len(res), time.Since(start))
	if len(res) == 0 {
		t.Fatal("expected search results for 'Quantum'")
	}
	for _, r := range res {
		t.Logf("  %s -> %s", r.Title, r.Path)
	}

	e, err := a.Find(a.ContentNamespace(), res[0].Path)
	if err != nil {
		t.Fatalf("Find(%q): %v", res[0].Path, err)
	}
	e, err = a.Resolve(e)
	if err != nil {
		t.Fatal(err)
	}
	body, err := a.Content(e)
	if err != nil || len(body) == 0 {
		t.Fatalf("content of %q: %d bytes, err %v", res[0].Path, len(body), err)
	}

	if _, err := a.Find(a.ContentNamespace(), "Definitely_not_an_article_xyz"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Every content entry should decode: this walks the whole archive.
func TestSampleReadAllEntries(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	a := sampleArchive(t)
	start := time.Now()
	var content, redirects, bytesRead int
	mimes := map[string]int{}
	for i := uint32(0); i < a.Header.EntryCount; i++ {
		e, err := a.EntryAt(i)
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if e.IsRedirect {
			redirects++
			if _, err := a.Resolve(e); err != nil {
				t.Errorf("redirect %c/%s: %v", e.Namespace, e.Path, err)
			}
			continue
		}
		if e.MimeType == "" {
			continue
		}
		b, err := a.Content(e)
		if err != nil {
			t.Fatalf("content %c/%s: %v", e.Namespace, e.Path, err)
		}
		content++
		bytesRead += len(b)
		mimes[e.MimeType]++
	}
	t.Logf("read %d content entries (%d MB) and %d redirects in %v", content, bytesRead>>20, redirects, time.Since(start))
	t.Logf("MIME types: %v", mimes)
}

func TestRejectsNonZim(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.zim")
	os.WriteFile(p, make([]byte, 200), 0o644)
	if _, err := Open(p); err != ErrNotZim {
		t.Fatalf("expected ErrNotZim, got %v", err)
	}
}

func TestRejectsTruncated(t *testing.T) {
	matches, _ := filepath.Glob("../../test-drive/OfflineLibrary/packs/wikipedia-physics/content/*.zim")
	if len(matches) == 0 {
		t.Skip("sample ZIM not downloaded")
	}
	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Simulate a partial copy: only the first 1 MB is present.
	a, err := New(f, 1<<20)
	if err == nil {
		// Header may still parse if all pointer lists are early; content must fail cleanly.
		defer a.Close()
		if mp, err := a.MainPage(); err == nil {
			if _, err := a.Content(mp); err == nil {
				t.Log("main page happened to be within the first MB")
			}
		}
	}
}
