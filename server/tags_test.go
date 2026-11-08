package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func tagsState(t *testing.T, body string) map[string][]string {
	t.Helper()
	var tf tagsFile
	if err := json.Unmarshal([]byte(body), &tf); err != nil {
		t.Fatalf("bad JSON %q: %v", body, err)
	}
	return tf.Packs
}

func TestNormalizeTags(t *testing.T) {
	got := normalizeTags([]string{" Hunting ", "hunting", "First   Aid", "", "  ", "bad\x07tag", strings.Repeat("x", 41), "recipes"})
	want := []string{"first aid", "hunting", "recipes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTagsRoundTrip(t *testing.T) {
	s, h := newMessagesServer(t)

	if rec := do(t, h, "GET", "/api/tags", ""); rec.Code != 200 || len(tagsState(t, rec.Body.String())) != 0 {
		t.Fatalf("empty: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, "POST", "/api/tags/pack/wiki-en", `["Technology", "reference", "technology"]`); rec.Code != 200 {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	do(t, h, "POST", "/api/tags/pack/field-guide", `["hunting", "reference"]`)

	rec := do(t, h, "GET", "/api/tags", "")
	want := map[string][]string{"wiki-en": {"reference", "technology"}, "field-guide": {"hunting", "reference"}}
	if got := tagsState(t, rec.Body.String()); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// Rename merges into an existing tag; rename to "" deletes it.
	do(t, h, "POST", "/api/tags/rename", `{"from":"technology","to":"Reference"}`)
	rec = do(t, h, "POST", "/api/tags/rename", `{"from":"hunting","to":""}`)
	want = map[string][]string{"wiki-en": {"reference"}, "field-guide": {"reference"}}
	if got := tagsState(t, rec.Body.String()); !reflect.DeepEqual(got, want) {
		t.Fatalf("after rename: got %v, want %v", got, want)
	}

	// Clearing a pack's tags removes it from the file.
	rec = do(t, h, "POST", "/api/tags/pack/wiki-en", `[]`)
	if got := tagsState(t, rec.Body.String()); len(got) != 1 {
		t.Fatalf("after clear: %v", got)
	}
	if _, err := os.Stat(filepath.Join(s.root, "data", "tags.json.tmp")); err == nil {
		t.Error("temporary file left behind")
	}
}

func TestTagsRejectBadInput(t *testing.T) {
	s, h := newMessagesServer(t)
	var distinct []string
	for i := range maxTagsPerPack + 1 {
		distinct = append(distinct, strings.Repeat("x", i+1))
	}
	tooMany, _ := json.Marshal(distinct)

	cases := []struct{ url, body string }{
		{"/api/tags/pack/..%2fx", `["a"]`},
		{"/api/tags/pack/Wiki", `["a"]`}, // pack ids are lowercase
		{"/api/tags/pack/wiki", `"a"`},
		{"/api/tags/pack/wiki", string(tooMany)},
		{"/api/tags/rename", `{"from":"","to":"x"}`},
		{"/api/tags/rename", `{"from":"a","to":"   "}`},
	}
	for _, c := range cases {
		if rec := do(t, h, "POST", c.url, c.body); rec.Code != 400 {
			t.Errorf("POST %s %s: got %d, want 400", c.url, c.body, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(s.root, "data")); err == nil {
		t.Error("rejected requests should not create files")
	}
}

func TestTagsDamagedFileIsNotOverwritten(t *testing.T) {
	s, h := newMessagesServer(t)
	path := filepath.Join(s.root, "data", "tags.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"packs": {"wiki": ["a"`), 0o644)

	if rec := do(t, h, "POST", "/api/tags/pack/wiki", `["b"]`); rec.Code != 409 {
		t.Fatalf("want 409, got %d", rec.Code)
	}
	if b, _ := os.ReadFile(path); string(b) != `{"packs": {"wiki": ["a"` {
		t.Fatalf("damaged file was overwritten: %s", b)
	}
}

func TestTagsNeedLibrary(t *testing.T) {
	s, h := newMessagesServer(t)
	os.Remove(filepath.Join(s.root, "library.json"))
	if rec := do(t, h, "POST", "/api/tags/pack/wiki", `["a"]`); rec.Code != 503 {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}
