package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newMessagesServer(t *testing.T) (*server, http.Handler) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "library.json"), []byte(`{"schemaVersion":1,"packs":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{root: root, dev: true} // dev skips the cookie check
	s.lib = Scan(root)
	return s, s.routes()
}

func do(t *testing.T, h http.Handler, method, url, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMessagesRoundTrip(t *testing.T) {
	s, h := newMessagesServer(t)

	if rec := do(t, h, "GET", "/api/messages/1a2b3c4d", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty history: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, "POST", "/api/messages/1a2b3c4d", `[{"t":"msg","id":1,"text":"hi\nthere"}, {"t":"status","id":1,"status":"delivered"}]`); rec.Code != 204 {
		t.Fatalf("append: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, "POST", "/api/messages/1a2b3c4d", `[{"t":"msg","id":2,"text":"second"}]`); rec.Code != 204 {
		t.Fatalf("append 2: %d", rec.Code)
	}

	// Simulate a write cut short by the drive disconnecting.
	path := filepath.Join(s.root, "data", "messages", "1a2b3c4d.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"t":"msg","id":3,"te`)
	f.Close()

	rec := do(t, h, "GET", "/api/messages/1a2b3c4d", "")
	body := rec.Body.String()
	if rec.Code != 200 || strings.Count(body, `"t":"`) != 3 || strings.Contains(body, `"id":3`) {
		t.Fatalf("history: %d %s", rec.Code, body)
	}
	if !strings.Contains(body, `hi\nthere`) {
		t.Fatalf("newline in text should survive as an escape: %s", body)
	}
}

func TestMessagesRejectsBadInput(t *testing.T) {
	s, h := newMessagesServer(t)
	cases := []struct {
		method, url, body string
		want              int
	}{
		{"GET", "/api/messages/..%2f..%2fx", "", 400},
		{"GET", "/api/messages/1A2B3C4D", "", 400}, // uppercase isn't accepted
		{"GET", "/api/messages/1a2b3c4", "", 400},  // too short
		{"POST", "/api/messages/1a2b3c4d", `{"t":"msg"}`, 400},
		{"POST", "/api/messages/1a2b3c4d", `[1, 2]`, 400},
		{"POST", "/api/messages/1a2b3c4d", `["x"]`, 400},
		{"POST", "/api/messages/1a2b3c4d", `[{"text":"` + strings.Repeat("x", maxMessageRecord) + `"}]`, 413},
	}
	for _, c := range cases {
		if rec := do(t, h, c.method, c.url, c.body); rec.Code != c.want {
			t.Errorf("%s %s: got %d, want %d (%s)", c.method, c.url, rec.Code, c.want, rec.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(s.root, "data")); err == nil {
		t.Error("rejected requests should not create files")
	}
}

func TestMessagesNeedLibrary(t *testing.T) {
	s, h := newMessagesServer(t)
	os.Remove(filepath.Join(s.root, "library.json")) // drive unplugged
	if rec := do(t, h, "POST", "/api/messages/1a2b3c4d", `[{"t":"msg"}]`); rec.Code != 503 {
		t.Fatalf("want 503, got %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(s.root, "data")); err == nil {
		t.Error("nothing should be written without a library")
	}
}
