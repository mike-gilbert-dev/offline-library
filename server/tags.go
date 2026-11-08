package main

// User tags for packs. Tags belong to the person using the drive, not to the
// packs, so they live in OfflineLibrary/data/tags.json rather than in
// pack.json: replacing or re-downloading a pack keeps its tags, and pack
// manifests stay untouched.
//
// Tags are keyed by packId. Tags for a pack that isn't on the drive right now
// are kept, so unplugging a second drive or moving a pack doesn't lose them.
//
// The file is small and rewritten whole: written to a temporary file next to
// it, then renamed over it, so a drive that drops out mid-write leaves either
// the old file or the new one, never half of one.

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	maxTagRunes    = 40
	maxTagsPerPack = 30
	maxTagsBody    = 16 << 10
)

type tagsFile struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Packs         map[string][]string `json:"packs"`
}

var tagsMu sync.Mutex

func (s *server) tagsPath() string { return filepath.Join(s.root, "data", "tags.json") }

// normalizeTag trims, collapses inner whitespace and lowercases a tag, so
// "Hunting", " hunting " and "HUNTING" are the same tag. It returns "" for
// tags that are empty, too long or contain control characters.
func normalizeTag(tag string) string {
	tag = strings.ToLower(strings.Join(strings.Fields(tag), " "))
	if tag == "" || utf8.RuneCountInString(tag) > maxTagRunes || !utf8.ValidString(tag) {
		return ""
	}
	for _, r := range tag {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return tag
}

// normalizeTags returns the valid tags, sorted and without duplicates.
func normalizeTags(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		if n := normalizeTag(t); n != "" {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// loadTags reads tags.json. A missing file means no tags yet. A file that
// can't be parsed is reported, rather than treated as empty, so the next save
// doesn't silently wipe tags the user can still recover.
func (s *server) loadTags() (tagsFile, error) {
	tf := tagsFile{SchemaVersion: 1, Packs: map[string][]string{}}
	data, err := os.ReadFile(s.tagsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return tf, nil
	}
	if err != nil {
		return tf, err
	}
	if err := json.Unmarshal(data, &tf); err != nil {
		return tf, errTagsDamaged
	}
	if tf.Packs == nil {
		tf.Packs = map[string][]string{}
	}
	for id, tags := range tf.Packs {
		if tags = normalizeTags(tags); len(tags) == 0 || !packIDPattern.MatchString(id) {
			delete(tf.Packs, id)
		} else {
			tf.Packs[id] = tags
		}
	}
	return tf, nil
}

var errTagsDamaged = errors.New("tags file is damaged")

func (s *server) saveTags(tf tagsFile) error {
	tf.SchemaVersion = 1
	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return err
	}
	path := s.tagsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(data, '\n'))
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// tagsError writes a user-facing error for a failed load or save.
func tagsError(w http.ResponseWriter, err error) {
	if errors.Is(err, errTagsDamaged) {
		http.Error(w, "The tags file on the drive (OfflineLibrary/data/tags.json) is damaged, so tags can't be changed. Fix or delete that file, then try again.", http.StatusConflict)
		return
	}
	http.Error(w, "Tags can't be saved to the drive right now. Check that the drive is connected.", http.StatusServiceUnavailable)
}

func (s *server) handleGetTags(w http.ResponseWriter, r *http.Request) {
	if !s.libraryPresent() {
		http.Error(w, "library not available", http.StatusServiceUnavailable)
		return
	}
	tagsMu.Lock()
	tf, err := s.loadTags()
	tagsMu.Unlock()
	if err != nil {
		tagsError(w, err)
		return
	}
	writeJSON(w, tf)
}

// handleSetPackTags replaces one pack's tags with the JSON array in the body.
func (s *server) handleSetPackTags(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("pack")
	if !packIDPattern.MatchString(id) {
		http.Error(w, "bad pack id", http.StatusBadRequest)
		return
	}
	var tags []string
	if !readJSONBody(w, r, &tags) {
		return
	}
	tags = normalizeTags(tags)
	if len(tags) > maxTagsPerPack {
		http.Error(w, "A pack can have at most 30 tags.", http.StatusBadRequest)
		return
	}
	s.updateTags(w, func(tf *tagsFile) {
		if len(tags) == 0 {
			delete(tf.Packs, id)
		} else {
			tf.Packs[id] = tags
		}
	})
}

// handleRenameTag renames a tag on every pack, merging it into the new name
// if that tag already exists. Renaming to "" deletes the tag everywhere.
func (s *server) handleRenameTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !readJSONBody(w, r, &req) {
		return
	}
	from := normalizeTag(req.From)
	to := normalizeTag(req.To)
	if from == "" || (req.To != "" && to == "") {
		http.Error(w, "Tags must be 1 to 40 characters.", http.StatusBadRequest)
		return
	}
	s.updateTags(w, func(tf *tagsFile) {
		for id, tags := range tf.Packs {
			if !slices.Contains(tags, from) {
				continue
			}
			tags = slices.DeleteFunc(slices.Clone(tags), func(t string) bool { return t == from })
			if to != "" {
				tags = normalizeTags(append(tags, to))
			}
			if len(tags) == 0 {
				delete(tf.Packs, id)
			} else {
				tf.Packs[id] = tags
			}
		}
	})
}

// updateTags loads, changes and saves the tags file under the lock, then
// responds with the saved state.
func (s *server) updateTags(w http.ResponseWriter, change func(*tagsFile)) {
	if !s.libraryPresent() {
		http.Error(w, "library not available", http.StatusServiceUnavailable)
		return
	}
	tagsMu.Lock()
	defer tagsMu.Unlock()
	tf, err := s.loadTags()
	if err == nil {
		change(&tf)
		err = s.saveTags(tf)
	}
	if err != nil {
		tagsError(w, err)
		return
	}
	writeJSON(w, tf)
}

func readJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTagsBody))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		http.Error(w, "bad JSON", http.StatusBadRequest)
		return false
	}
	return true
}
