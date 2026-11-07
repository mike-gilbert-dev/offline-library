package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"v0.2.1", "v0.2.0", true},
		{"v0.10.0", "v0.9.9", true}, // numeric, not string, comparison
		{"v1.0.0", "v0.99.99", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.1.9", "v0.2.0", false},
		{"v0.3.0", "dev", false},
		{"0.3.0", "v0.2.0", false}, // tags must start with v
		{"v0.3.0-beta", "v0.2.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.candidate, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}

// fakeGitHub serves /repos/<repo>/releases/latest with the given status and body.
func fakeGitHub(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+updateRepo+"/releases/latest" {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "offline-library/") {
			t.Errorf("missing User-Agent")
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	old := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = old; srv.Close() })
}

func checkUpdate(t *testing.T, current string) (int, map[string]any) {
	t.Helper()
	oldVersion := version
	version = current
	t.Cleanup(func() { version = oldVersion })
	t.Setenv("OLIB_GITHUB_TOKEN", "")
	borrowGHToken = false
	t.Cleanup(func() { borrowGHToken = true })

	s := &server{root: t.TempDir(), dev: true}
	s.lib = Scan(s.root)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/update/check", nil))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad JSON %q: %v", rec.Body.String(), err)
	}
	return rec.Code, out
}

func TestUpdateCheck(t *testing.T) {
	fakeGitHub(t, 200, `{"tag_name":"v0.3.0","name":"v0.3.0","body":"New things","html_url":"https://example/r","published_at":"2026-11-07T12:00:00Z"}`)

	code, out := checkUpdate(t, "v0.2.0")
	if code != 200 || out["updateAvailable"] != true || out["latest"] != "v0.3.0" || out["notes"] != "New things" {
		t.Fatalf("older install: %d %v", code, out)
	}
	if _, out := checkUpdate(t, "v0.3.0"); out["updateAvailable"] != false {
		t.Fatalf("same version should be up to date: %v", out)
	}
	if _, out := checkUpdate(t, "dev"); out["updateAvailable"] != false || out["devBuild"] != true {
		t.Fatalf("dev build: %v", out)
	}
}

func TestUpdateCheckErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{404, `{"message":"Not Found"}`, "private"},
		{403, `{"message":"rate limit"}`, "limiting"},
		{502, ``, "problem"},
		{200, `{"tag_name":"nightly"}`, "version number"},
		{200, `{"tag_name":"v9.0.0","prerelease":true}`, "version number"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			fakeGitHub(t, c.status, c.body)
			code, out := checkUpdate(t, "v0.2.0")
			msg, _ := out["error"].(string)
			if code != http.StatusBadGateway || !strings.Contains(msg, c.want) {
				t.Fatalf("got %d %q, want 502 containing %q", code, msg, c.want)
			}
		})
	}
}

func TestBrowserEnvDropsToken(t *testing.T) {
	got := browserEnv([]string{"PATH=/bin", "OLIB_GITHUB_TOKEN=secret", "olib_update_api=x", "HOME=/h"})
	if strings.Join(got, ";") != "PATH=/bin;HOME=/h" {
		t.Fatalf("got %v", got)
	}
}

func TestUpdateCheckOffline(t *testing.T) {
	old := githubAPI
	githubAPI = "http://127.0.0.1:1" // nothing listens on port 1
	t.Cleanup(func() { githubAPI = old })
	code, out := checkUpdate(t, "v0.2.0")
	if msg, _ := out["error"].(string); code != http.StatusBadGateway || !strings.Contains(msg, "internet") {
		t.Fatalf("offline: %d %v", code, out)
	}
}
