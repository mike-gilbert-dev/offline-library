package main

// Update check: compares this launcher's version with the latest GitHub
// release. It runs only when the user presses "Check for updates" — the
// library never contacts the internet on its own — and it is done here in the
// launcher so the page itself keeps its strict no-network CSP.
//
// The repository is private for now, so GitHub only shows its releases to an
// authenticated request. No credentials are ever built into the launcher: a
// token is used only if OLIB_GITHUB_TOKEN is set in the environment, or, in
// "dev" builds, borrowed from a signed-in GitHub CLI (`gh auth token`).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const updateRepo = "mike-gilbert-dev/offline-library"

// githubAPI is a variable so tests can point it at a fake server.
var githubAPI = "https://api.github.com"

// borrowGHToken allows dev builds to use the GitHub CLI's login; tests turn
// it off so they never touch real credentials.
var borrowGHToken = true

type releaseInfo struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt time.Time      `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []releaseAsset `json:"assets"`
}

// releaseAsset is a file attached to a release. URL is the API address,
// which (with Accept: application/octet-stream) works for private and
// public repositories alike.
type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

type updateCheck struct {
	Current         string    `json:"current"`
	Latest          string    `json:"latest"`
	UpdateAvailable bool      `json:"updateAvailable"`
	DevBuild        bool      `json:"devBuild"`
	Notes           string    `json:"notes"`
	URL             string    `json:"url"`
	PublishedAt     time.Time `json:"publishedAt"`
}

// updateError is the friendly message shown in Settings when a check fails.
type updateError struct {
	Error string `json:"error"`
}

func (s *server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	rel, status, err := fetchLatestRelease(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(updateError{Error: friendlyUpdateError(status, err)})
		return
	}
	writeJSON(w, compareRelease(version, rel))
}

func compareRelease(current string, rel *releaseInfo) updateCheck {
	res := updateCheck{
		Current:     current,
		Latest:      rel.TagName,
		DevBuild:    current == "dev",
		Notes:       rel.Body,
		URL:         rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
	}
	if !res.DevBuild {
		res.UpdateAvailable = newerVersion(rel.TagName, current)
	}
	return res
}

// fetchLatestRelease returns the newest published, non-prerelease release.
// The HTTP status is returned alongside errors so they can be explained.
func fetchLatestRelease(ctx context.Context) (*releaseInfo, int, error) {
	return fetchRelease(ctx, "/releases/latest")
}

// fetchReleaseByTag returns one release, e.g. the version being installed.
func fetchReleaseByTag(ctx context.Context, tag string) (*releaseInfo, int, error) {
	if parseVersion(tag) == nil {
		return nil, 0, fmt.Errorf("%q is not a release version", tag)
	}
	return fetchRelease(ctx, "/releases/tags/"+tag)
}

// apiBase is GitHub's API, unless OLIB_UPDATE_API points elsewhere (used
// to test the updater against a local fake; signatures are still required).
func apiBase() string {
	if v := strings.TrimRight(os.Getenv("OLIB_UPDATE_API"), "/"); v != "" {
		return v
	}
	return githubAPI
}

// githubRequest builds a GET with the headers GitHub expects, plus a token
// when one is available (see githubToken).
func githubRequest(ctx context.Context, url, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "offline-library/"+version)
	if tok := githubToken(ctx); tok != "" {
		// Go drops this header if GitHub redirects the download to another
		// host (asset storage), so the token only ever goes to GitHub.
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

func fetchRelease(ctx context.Context, path string) (*releaseInfo, int, error) {
	req, err := githubRequest(ctx, apiBase()+"/repos/"+updateRepo+path, "application/vnd.github+json")
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var rel releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("unexpected reply from GitHub: %w", err)
	}
	if rel.Draft || rel.Prerelease || parseVersion(rel.TagName) == nil {
		return nil, resp.StatusCode, errors.New("the latest release has no usable version number")
	}
	return &rel, resp.StatusCode, nil
}

func githubToken(ctx context.Context) string {
	if tok := strings.TrimSpace(os.Getenv("OLIB_GITHUB_TOKEN")); tok != "" {
		return tok
	}
	if version != "dev" || !borrowGHToken {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func friendlyUpdateError(status int, err error) string {
	switch {
	case status == http.StatusNotFound:
		return "No published releases were found. While the project's GitHub repository is private, updates can't be checked from here."
	case status == http.StatusForbidden || status == http.StatusTooManyRequests:
		return "GitHub is limiting requests right now. Try again in a little while."
	case status >= 500:
		return "GitHub had a problem answering. Try again later."
	case status == 0:
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) && netErr.Timeout() {
			return "Checking for updates timed out. Check the internet connection and try again."
		}
		return "Couldn't reach GitHub. Updates can only be checked while connected to the internet."
	default:
		return "Couldn't check for updates: " + err.Error()
	}
}

var versionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// parseVersion reads "v1.2.3"; anything else returns nil.
func parseVersion(v string) []int {
	m := versionPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return nil
	}
	out := make([]int, 3)
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// newerVersion reports whether candidate is a higher version than current.
func newerVersion(candidate, current string) bool {
	a, b := parseVersion(candidate), parseVersion(current)
	if a == nil || b == nil {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
