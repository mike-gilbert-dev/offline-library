package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRelease serves a release (tag, assets) like GitHub's API, with a
// manifest signed by a throwaway key. tamper lets a test corrupt an asset
// after the manifest was made.
type fakeRelease struct {
	tag      string
	files    map[string][]byte
	manifest []byte
	sig      []byte
	tamper   map[string][]byte
	served   map[string]int
}

func newFakeRelease(t *testing.T, tag string, files map[string][]byte) *fakeRelease {
	t.Helper()
	sign := withTestKey(t)
	m := releaseManifest{Schema: 1, Version: tag, Files: map[string]releaseFile{}}
	for name, data := range files {
		sum := sha256.Sum256(data)
		m.Files[name] = releaseFile{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	}
	manifest, _ := json.Marshal(m)
	r := &fakeRelease{tag: tag, files: files, manifest: manifest, sig: sign(manifest), tamper: map[string][]byte{}, served: map[string]int{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/repos/"+updateRepo+"/releases/tags/"+r.tag:
			var assets []releaseAsset
			add := func(name string) {
				assets = append(assets, releaseAsset{Name: name, URL: "http://" + req.Host + "/asset/" + name})
			}
			for name := range r.files {
				add(name)
			}
			if r.manifest != nil {
				add("release.json")
			}
			if r.sig != nil {
				add("release.json.sig")
			}
			json.NewEncoder(w).Encode(releaseInfo{TagName: r.tag, Assets: assets})
		case strings.HasPrefix(req.URL.Path, "/asset/"):
			name := strings.TrimPrefix(req.URL.Path, "/asset/")
			if req.Header.Get("Accept") != "application/octet-stream" {
				t.Errorf("asset %s requested without octet-stream Accept", name)
			}
			r.served[name]++
			switch {
			case name == "release.json":
				w.Write(r.manifest)
			case name == "release.json.sig":
				w.Write(r.sig)
			case r.tamper[name] != nil:
				w.Write(r.tamper[name])
			default:
				w.Write(r.files[name])
			}
		default:
			http.NotFound(w, req)
		}
	}))
	old := githubAPI
	githubAPI = srv.URL
	borrowGHToken = false
	t.Setenv("OLIB_UPDATE_API", "")
	t.Setenv("OLIB_GITHUB_TOKEN", "")
	t.Cleanup(func() { githubAPI = old; borrowGHToken = true; srv.Close() })
	return r
}

// installDir creates a folder with "old" launchers and a library folder.
func installDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"start-windows.exe", "start-linux"} {
		os.WriteFile(filepath.Join(dir, name), []byte("old "+name), 0o755)
	}
	os.MkdirAll(filepath.Join(dir, "OfflineLibrary"), 0o755)
	os.WriteFile(filepath.Join(dir, "OfflineLibrary", "library.json"), []byte(`{"packs":[]}`), 0o644)
	return dir
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func install(t *testing.T, dir string) error {
	return installRelease(context.Background(), "v0.3.0", dir, "start-windows.exe", func(int64, int64) {}, func() {})
}

func TestInstallRelease(t *testing.T) {
	newFakeRelease(t, "v0.3.0", map[string][]byte{
		"start-windows.exe": []byte("new windows launcher"),
		"start-linux":       []byte("new linux launcher"),
		"start-mac-intel":   []byte("new mac launcher"), // not present before
	})
	dir := installDir(t)
	if err := install(t, dir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"start-windows.exe":           "new windows launcher",
		"start-linux":                 "new linux launcher",
		"start-mac-intel":             "new mac launcher",
		"start-windows.exe.old":       "old start-windows.exe",
		"start-linux.old":             "old start-linux",
		"OfflineLibrary/library.json": `{"packs":[]}`,
		"start-windows.exe.download":  "<missing>",
		"start-mac-intel.old":         "<missing>",
	} {
		if got := read(t, filepath.Join(dir, name)); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	cleanupAfterUpdate(dir)
	if _, err := os.Stat(filepath.Join(dir, "start-windows.exe.old")); err == nil {
		t.Error("cleanup should remove .old files")
	}
	if got := read(t, filepath.Join(dir, "OfflineLibrary", "library.json")); got != `{"packs":[]}` {
		t.Error("cleanup must not touch the library")
	}
}

// Every failure must leave the existing launchers exactly as they were.
func assertUntouched(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, ","); got != "OfflineLibrary,start-linux,start-windows.exe" {
		t.Errorf("folder changed: %s", got)
	}
	if read(t, filepath.Join(dir, "start-windows.exe")) != "old start-windows.exe" {
		t.Error("launcher was modified")
	}
}

func TestInstallRejectsTamperedFile(t *testing.T) {
	r := newFakeRelease(t, "v0.3.0", map[string][]byte{
		"start-windows.exe": []byte("new windows launcher"),
		"start-linux":       []byte("new linux launcher"),
	})
	r.tamper["start-linux"] = []byte("evil linux launcher") // same size, different bytes
	dir := installDir(t)
	if err := install(t, dir); err == nil || !strings.Contains(err.Error(), "checksum") && !strings.Contains(err.Error(), "size") {
		t.Fatalf("want checksum/size error, got %v", err)
	}
	assertUntouched(t, dir)
}

func TestInstallRejectsBadSignature(t *testing.T) {
	r := newFakeRelease(t, "v0.3.0", map[string][]byte{"start-windows.exe": []byte("new")})
	r.manifest = []byte(strings.Replace(string(r.manifest), `"v0.3.0"`, `"v0.3.0" `, 1)) // any change breaks it
	dir := installDir(t)
	if err := install(t, dir); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("want signature error, got %v", err)
	}
	if r.served["start-windows.exe"] != 0 {
		t.Error("launchers must not be downloaded before the manifest verifies")
	}
	assertUntouched(t, dir)
}

func TestInstallRequiresSignature(t *testing.T) {
	r := newFakeRelease(t, "v0.3.0", map[string][]byte{"start-windows.exe": []byte("new")})
	r.sig = nil
	dir := installDir(t)
	if err := install(t, dir); err == nil || !strings.Contains(err.Error(), "verified") {
		t.Fatalf("want missing-signature error, got %v", err)
	}
	assertUntouched(t, dir)
}

func TestInstallRequiresRunningLauncher(t *testing.T) {
	newFakeRelease(t, "v0.3.0", map[string][]byte{"start-linux": []byte("new")})
	dir := installDir(t)
	if err := install(t, dir); err == nil || !strings.Contains(err.Error(), "start-windows.exe") {
		t.Fatalf("want error naming the running launcher, got %v", err)
	}
	assertUntouched(t, dir)
}

func TestSwapRollsBack(t *testing.T) {
	dir := installDir(t)
	os.WriteFile(filepath.Join(dir, "start-windows.exe.download"), []byte("new"), 0o755)
	// start-linux.download is missing, so its swap fails after Windows' succeeded.
	if err := swapFiles(dir, []string{"start-windows.exe", "start-linux"}); err == nil {
		t.Fatal("expected failure")
	}
	os.Remove(filepath.Join(dir, "start-windows.exe.download"))
	assertUntouched(t, dir)
}

func TestRestartArgs(t *testing.T) {
	got := strings.Join(restartArgs([]string{"-library", "X", "-restarted", "-dev"}), " ")
	if got != "-library X -dev -restarted" {
		t.Fatalf("got %q", got)
	}
}
