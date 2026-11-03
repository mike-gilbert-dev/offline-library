package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SupportedSchema is the manifest schemaVersion this build understands.
const SupportedSchema = 1

// supportedFormats lists pack formats this build can open. Packs with any
// other format are shown as "unsupported" rather than treated as broken.
var supportedFormats = map[string]bool{
	"pdf": true,
	"zim": true,
}

var packIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ---- Manifest formats (see docs in README) ----

type LibraryManifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	LibraryID     string   `json:"libraryId"`
	Title         string   `json:"title"`
	Packs         []string `json:"packs"`
}

type FileEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256,omitempty"`
}

type Source struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	License     string `json:"license"`
	Attribution string `json:"attribution"`
}

type PackManifest struct {
	SchemaVersion int         `json:"schemaVersion"`
	PackID        string      `json:"packId"`
	Title         string      `json:"title"`
	Description   string      `json:"description,omitempty"`
	Category      string      `json:"category"`
	Format        string      `json:"format"`
	Version       string      `json:"version"`
	Languages     []string    `json:"languages"`
	ContentDate   string      `json:"contentDate"`
	Entrypoint    string      `json:"entrypoint"`
	Files         []FileEntry `json:"files"`
	Source        Source      `json:"source"`
}

// ---- Scan results ----

type PackStatus string

const (
	StatusOK          PackStatus = "ok"          // valid and openable
	StatusUnsupported PackStatus = "unsupported" // valid, but this build can't open its format
	StatusIncomplete  PackStatus = "incomplete"  // files missing or wrong size (partial copy, unplugged drive)
	StatusInvalid     PackStatus = "invalid"     // manifest is malformed or unsafe
)

type Pack struct {
	ManifestPath string        `json:"manifestPath"`
	Status       PackStatus    `json:"status"`
	Problems     []string      `json:"problems"`
	Manifest     *PackManifest `json:"manifest,omitempty"`
	TotalBytes   int64         `json:"totalBytes"`

	// files maps each declared relative path to its verified absolute path.
	// Only populated for packs with StatusOK; the content endpoint serves
	// nothing else.
	files map[string]string
}

type Library struct {
	Root          string    `json:"root"`
	Available     bool      `json:"available"`
	Problems      []string  `json:"problems"`
	SchemaVersion int       `json:"schemaVersion,omitempty"`
	LibraryID     string    `json:"libraryId,omitempty"`
	Title         string    `json:"title,omitempty"`
	Packs         []*Pack   `json:"packs"`
	ScannedAt     time.Time `json:"scannedAt"`
}

func (l *Library) FindPack(id string) *Pack {
	for _, p := range l.Packs {
		if p.Manifest != nil && p.Manifest.PackID == id {
			return p
		}
	}
	return nil
}

// Scan reads library.json at root and validates every pack it lists.
// It never writes to the library and never panics on bad input.
func Scan(root string) *Library {
	lib := &Library{Root: root, Problems: []string{}, Packs: []*Pack{}, ScannedAt: time.Now()}

	data, err := os.ReadFile(filepath.Join(root, "library.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			lib.Problems = append(lib.Problems, "No library.json was found here. If the library is on an external drive, check that the drive is connected.")
		} else {
			lib.Problems = append(lib.Problems, fmt.Sprintf("library.json could not be read: %v", err))
		}
		return lib
	}
	lib.Available = true

	var m LibraryManifest
	if err := json.Unmarshal(data, &m); err != nil {
		lib.Problems = append(lib.Problems, fmt.Sprintf("library.json is not valid JSON: %v", err))
		return lib
	}
	lib.SchemaVersion, lib.LibraryID, lib.Title = m.SchemaVersion, m.LibraryID, m.Title
	if lib.Title == "" {
		lib.Title = "Untitled library"
	}
	if m.SchemaVersion == 0 {
		lib.Problems = append(lib.Problems, "library.json is missing schemaVersion.")
		return lib
	}
	if m.SchemaVersion != SupportedSchema {
		lib.Problems = append(lib.Problems, fmt.Sprintf("library.json uses schema version %d, but this app reads version %d. A newer version of the app may be needed.", m.SchemaVersion, SupportedSchema))
		return lib
	}
	if m.LibraryID == "" {
		lib.Problems = append(lib.Problems, "library.json is missing libraryId.")
	}

	seen := map[string]string{} // packId -> manifest path that claimed it first
	for _, rel := range m.Packs {
		p := scanPack(root, rel)
		if p.Manifest != nil && p.Manifest.PackID != "" {
			id := p.Manifest.PackID
			if first, dup := seen[id]; dup {
				p.Status = StatusInvalid
				p.files = nil
				p.Problems = append(p.Problems, fmt.Sprintf("Pack ID %q is already used by %s.", id, first))
			} else {
				seen[id] = rel
			}
		}
		lib.Packs = append(lib.Packs, p)
	}
	return lib
}

func scanPack(root, rel string) *Pack {
	p := &Pack{ManifestPath: rel, Problems: []string{}}
	fail := func(s PackStatus, format string, args ...any) *Pack {
		p.Status = s
		p.Problems = append(p.Problems, fmt.Sprintf(format, args...))
		return p
	}

	manifestPath, err := SafeJoin(root, rel)
	if err != nil {
		return fail(StatusInvalid, "Pack location %q is not allowed: %v.", rel, err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(StatusIncomplete, "Pack manifest not found at %s.", rel)
		}
		return fail(StatusIncomplete, "Pack manifest could not be read: %v", err)
	}

	var m PackManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fail(StatusInvalid, "pack.json is not valid JSON: %v", err)
	}
	p.Manifest = &m
	if m.SchemaVersion != SupportedSchema {
		return fail(StatusInvalid, "pack.json uses schema version %d, but this app reads version %d.", m.SchemaVersion, SupportedSchema)
	}

	var invalid, incomplete []string
	if !packIDPattern.MatchString(m.PackID) {
		invalid = append(invalid, "packId must be 1–64 lowercase letters, digits, '.', '_' or '-'.")
	}
	if strings.TrimSpace(m.Title) == "" {
		invalid = append(invalid, "title is required.")
	}
	if m.Format == "" {
		invalid = append(invalid, "format is required.")
	}
	if len(m.Files) == 0 {
		invalid = append(invalid, "files must list at least one file.")
	}

	packDir := filepath.Dir(manifestPath)
	files := map[string]string{}
	declared := map[string]bool{}
	for _, f := range m.Files {
		declared[f.Path] = true
		abs, err := SafeJoin(packDir, f.Path)
		if err != nil {
			invalid = append(invalid, fmt.Sprintf("File path %q is not allowed: %v.", f.Path, err))
			continue
		}
		if f.SizeBytes <= 0 {
			invalid = append(invalid, fmt.Sprintf("File %s needs a sizeBytes value.", f.Path))
			continue
		}
		st, err := os.Stat(abs)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			incomplete = append(incomplete, fmt.Sprintf("File %s is missing.", f.Path))
		case err != nil:
			incomplete = append(incomplete, fmt.Sprintf("File %s could not be read: %v", f.Path, err))
		case st.IsDir():
			invalid = append(invalid, fmt.Sprintf("%s is a folder, not a file.", f.Path))
		case st.Size() != f.SizeBytes:
			incomplete = append(incomplete, fmt.Sprintf("File %s is %d bytes but should be %d. The copy may be incomplete.", f.Path, st.Size(), f.SizeBytes))
		default:
			files[f.Path] = abs
			p.TotalBytes += st.Size()
		}
	}
	if m.Entrypoint == "" {
		invalid = append(invalid, "entrypoint is required.")
	} else if !declared[m.Entrypoint] {
		invalid = append(invalid, fmt.Sprintf("entrypoint %q is not listed in files.", m.Entrypoint))
	}

	p.Problems = append(p.Problems, invalid...)
	p.Problems = append(p.Problems, incomplete...)
	unsupported := m.Format != "" && !supportedFormats[m.Format]
	if unsupported {
		p.Problems = append(p.Problems, fmt.Sprintf("This version of the app can't open %q packs yet.", m.Format))
	}

	switch {
	case len(invalid) > 0:
		p.Status = StatusInvalid
	case len(incomplete) > 0:
		p.Status = StatusIncomplete
	case unsupported:
		p.Status = StatusUnsupported
	default:
		p.Status = StatusOK
		p.files = files
	}
	return p
}

// SafeJoin resolves a manifest-relative path inside base, rejecting anything
// that could escape it: absolute paths, drive letters, backslashes, "..",
// and (where the filesystem supports them) symlinks pointing outside base.
func SafeJoin(base, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("path is empty")
	}
	if strings.ContainsAny(rel, "\\:\x00") {
		return "", errors.New(`paths must use "/" and may not contain "\" or ":"`)
	}
	if strings.HasPrefix(rel, "/") {
		return "", errors.New("paths must be relative")
	}
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case "..":
			return "", errors.New(`paths may not contain ".."`)
		case "":
			return "", errors.New("paths may not contain empty segments")
		}
	}

	joined := filepath.Join(base, filepath.FromSlash(rel))
	if !within(base, joined) {
		return "", errors.New("path leaves the pack folder")
	}

	// Best-effort symlink check. If resolution fails (file missing, or a
	// volume that doesn't support it) we fall back to the lexical check
	// above; exFAT, the expected drive format, has no symlinks anyway.
	realBase, err1 := filepath.EvalSymlinks(base)
	realJoined, err2 := filepath.EvalSymlinks(joined)
	if err1 == nil && err2 == nil && !within(realBase, realJoined) {
		return "", errors.New("path resolves outside the pack folder (symbolic link?)")
	}
	return joined, nil
}

func within(base, target string) bool {
	r, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(r) {
		return false
	}
	return r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}
