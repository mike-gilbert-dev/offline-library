package main

// Installing an update. The steps, all in the folder the launcher runs from:
//
//  1. Download release.json + release.json.sig and verify the signature
//     (releasekey.go). Nothing else is downloaded unless it verifies.
//  2. Download every launcher the manifest lists to "<name>.download",
//     checking size and SHA-256 while streaming.
//  3. Swap: "<name>" -> "<name>.old", then "<name>.download" -> "<name>".
//     Windows allows renaming (not overwriting) a running program. If any
//     rename fails, everything already swapped is renamed back.
//  4. Restart into the new launcher (restart_*.go). The new version deletes
//     the .old files once it has started, so a version that fails to start
//     leaves the previous one in place to roll back to.
//
// Only plain file names from a signed manifest are ever written, so the
// OfflineLibrary folder (packs, message history) can't be touched.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxLauncherSize = 200 << 20 // sanity limit per file

type updateStatus struct {
	State   string `json:"state"` // idle, downloading, installing, restarting, error
	Version string `json:"version,omitempty"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Error   string `json:"error,omitempty"`
}

type updater struct {
	mu     sync.Mutex
	status updateStatus
}

func (u *updater) get() updateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status
}

func (u *updater) set(f func(*updateStatus)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	f(&u.status)
}

func (s *server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	st := s.updates.get()
	if st.State == "" {
		st.State = "idle"
	}
	writeJSON(w, st)
}

// handleUpdateInstall starts installing the given version in the background;
// the page follows progress through /api/update/status.
func (s *server) handleUpdateInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || parseVersion(req.Version) == nil {
		http.Error(w, "expected {\"version\":\"v1.2.3\"}", http.StatusBadRequest)
		return
	}
	if version == "dev" {
		http.Error(w, "Development builds can't be updated.", http.StatusConflict)
		return
	}
	if !newerVersion(req.Version, version) {
		http.Error(w, fmt.Sprintf("%s is not newer than the installed %s.", req.Version, version), http.StatusConflict)
		return
	}
	dir, self, err := launcherLocation()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	s.updates.mu.Lock()
	if st := s.updates.status.State; st == "downloading" || st == "installing" || st == "restarting" {
		s.updates.mu.Unlock()
		http.Error(w, "An update is already in progress.", http.StatusConflict)
		return
	}
	s.updates.status = updateStatus{State: "downloading", Version: req.Version}
	s.updates.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		progress := func(done, total int64) {
			s.updates.set(func(st *updateStatus) { st.Done, st.Total = done, total })
		}
		installing := func() { s.updates.set(func(st *updateStatus) { st.State = "installing" }) }
		if err := installRelease(ctx, req.Version, dir, self, progress, installing); err != nil {
			s.updates.set(func(st *updateStatus) { st.State, st.Error = "error", err.Error() })
			return
		}
		s.updates.set(func(st *updateStatus) { st.State = "restarting" })
		// Give the page a moment to see "restarting" before the server stops.
		time.Sleep(1500 * time.Millisecond)
		select {
		case s.restart <- struct{}{}:
		default:
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

// launcherLocation returns the folder and file name of the running launcher.
func launcherLocation() (dir, name string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", errors.New("can't tell where the library program is running from")
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe), filepath.Base(exe), nil
}

// installRelease downloads, verifies and swaps in the launchers of release
// tag into dir. self is the running launcher's file name, which must be part
// of the release so the restart picks up the new version.
func installRelease(ctx context.Context, tag, dir, self string, progress func(done, total int64), installing func()) error {
	rel, status, err := fetchReleaseByTag(ctx, tag)
	if err != nil {
		return errors.New(friendlyUpdateError(status, err))
	}
	assets := map[string]releaseAsset{}
	for _, a := range rel.Assets {
		assets[a.Name] = a
	}

	// 1. Signed manifest first.
	manifest, err := downloadSmall(ctx, assets, "release.json")
	if err != nil {
		return err
	}
	sig, err := downloadSmall(ctx, assets, "release.json.sig")
	if err != nil {
		return err
	}
	m, err := verifyReleaseManifest(manifest, sig, tag)
	if err != nil {
		return err
	}
	if _, ok := m.Files[self]; !ok {
		return fmt.Errorf("this release doesn't include %s (was the program renamed?)", self)
	}
	var total int64
	for name, f := range m.Files {
		if _, ok := assets[name]; !ok {
			return fmt.Errorf("the release is missing %s", name)
		}
		if f.Size > maxLauncherSize {
			return fmt.Errorf("%s is unexpectedly large", name)
		}
		total += f.Size
	}

	// 2. Download everything next to the current files, verifying as we go.
	var done int64
	progress(0, total)
	var downloaded []string
	cleanup := func() {
		for _, name := range downloaded {
			os.Remove(filepath.Join(dir, name+".download"))
		}
	}
	for name, f := range m.Files {
		downloaded = append(downloaded, name)
		if err := downloadVerified(ctx, assets[name], f, filepath.Join(dir, name+".download"), func(n int64) {
			progress(done+n, total)
		}); err != nil {
			cleanup()
			return err
		}
		done += f.Size
	}

	// 3. Swap, rolling back on any failure.
	installing()
	if err := swapFiles(dir, downloaded); err != nil {
		cleanup()
		return err
	}
	return nil
}

// downloadSmall fetches a small asset (manifest, signature) into memory.
func downloadSmall(ctx context.Context, assets map[string]releaseAsset, name string) ([]byte, error) {
	a, ok := assets[name]
	if !ok {
		return nil, fmt.Errorf("this release has no %s, so it can't be verified and won't be installed", name)
	}
	body, err := openAsset(ctx, a)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("downloading %s failed: %w", name, err)
	}
	return data, nil
}

func openAsset(ctx context.Context, a releaseAsset) (io.ReadCloser, error) {
	req, err := githubRequest(ctx, a.URL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New(friendlyUpdateError(0, err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("downloading %s failed (%s)", a.Name, resp.Status)
	}
	return resp.Body, nil
}

// downloadVerified streams an asset to path, refusing anything that isn't
// exactly the size and SHA-256 the signed manifest promises.
func downloadVerified(ctx context.Context, a releaseAsset, want releaseFile, path string, progress func(int64)) error {
	body, err := openAsset(ctx, a)
	if err != nil {
		return err
	}
	defer body.Close()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("can't write to the drive: %w", err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), &progressReader{r: io.LimitReader(body, want.Size+1), report: progress})
	syncErr := f.Sync()
	closeErr := f.Close()
	switch {
	case copyErr != nil:
		return fmt.Errorf("downloading %s failed: %w", a.Name, copyErr)
	case syncErr != nil || closeErr != nil:
		return fmt.Errorf("saving %s to the drive failed", a.Name)
	case n != want.Size:
		return fmt.Errorf("%s has the wrong size; the update was not installed", a.Name)
	case hex.EncodeToString(h.Sum(nil)) != want.SHA256:
		return fmt.Errorf("%s doesn't match its signed checksum; the update was not installed", a.Name)
	}
	return nil
}

type progressReader struct {
	r      io.Reader
	n      int64
	report func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	p.report(p.n)
	return n, err
}

// swapFiles moves each "<name>.download" into place, keeping the previous
// file as "<name>.old". On failure, everything is put back as it was.
func swapFiles(dir string, names []string) error {
	type done struct{ name, old string }
	var swapped []done
	rollback := func() {
		for i := len(swapped) - 1; i >= 0; i-- {
			cur := filepath.Join(dir, swapped[i].name)
			os.Remove(cur)
			if swapped[i].old != "" {
				os.Rename(swapped[i].old, cur)
			}
		}
	}
	for _, name := range names {
		cur := filepath.Join(dir, name)
		old := cur + ".old"
		os.Remove(old) // leftover from an earlier update
		hadOld := false
		if _, err := os.Stat(cur); err == nil {
			if err := os.Rename(cur, old); err != nil {
				rollback()
				return fmt.Errorf("couldn't replace %s: %w", name, err)
			}
			hadOld = true
		}
		if err := os.Rename(cur+".download", cur); err != nil {
			if hadOld {
				os.Rename(old, cur)
			}
			rollback()
			return fmt.Errorf("couldn't install %s: %w", name, err)
		}
		d := done{name: name}
		if hadOld {
			d.old = old
		}
		swapped = append(swapped, d)
	}
	return nil
}

// cleanupAfterUpdate removes the previous launchers once this version has
// started. On Windows the old process may still be exiting, so retry.
func cleanupAfterUpdate(dir string) {
	for attempt := 0; attempt < 30; attempt++ {
		left := 0
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, "start-") && (strings.HasSuffix(n, ".old") || strings.HasSuffix(n, ".download")) {
				if os.Remove(filepath.Join(dir, n)) != nil {
					left++
				}
			}
		}
		if left == 0 {
			return
		}
		time.Sleep(time.Second)
	}
}
