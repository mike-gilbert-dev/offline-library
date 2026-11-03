package main

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"offlinelibrary/server/zim"
)

// archiveSet keeps ZIM archives open between requests. Archives are opened
// lazily and all closed on rescan (e.g. after the drive is reconnected).
type archiveSet struct {
	mu   sync.Mutex
	open map[string]*zim.Archive // keyed by pack ID
}

func (as *archiveSet) get(pack *Pack) (*zim.Archive, error) {
	as.mu.Lock()
	defer as.mu.Unlock()
	id := pack.Manifest.PackID
	if a, ok := as.open[id]; ok {
		return a, nil
	}
	path, ok := pack.files[pack.Manifest.Entrypoint]
	if !ok {
		return nil, errors.New("entrypoint not available")
	}
	a, err := zim.Open(path)
	if err != nil {
		return nil, err
	}
	if as.open == nil {
		as.open = map[string]*zim.Archive{}
	}
	as.open[id] = a
	return a, nil
}

// drop closes one archive, e.g. after a read error suggests the drive went away.
func (as *archiveSet) drop(id string) {
	as.mu.Lock()
	defer as.mu.Unlock()
	if a, ok := as.open[id]; ok {
		a.Close()
		delete(as.open, id)
	}
}

func (as *archiveSet) closeAll() {
	as.mu.Lock()
	defer as.mu.Unlock()
	for id, a := range as.open {
		a.Close()
		delete(as.open, id)
	}
}

// zimPack returns the pack and its open archive, or writes an error.
func (s *server) zimPack(w http.ResponseWriter, id string) (*Pack, *zim.Archive, bool) {
	s.mu.RLock()
	pack := s.lib.FindPack(id)
	s.mu.RUnlock()
	if pack == nil || pack.Status != StatusOK || pack.Manifest.Format != "zim" {
		http.NotFound(w, nil)
		return nil, nil, false
	}
	a, err := s.archives.get(pack)
	if err != nil {
		unavailable(w, err)
		return nil, nil, false
	}
	return pack, a, true
}

func unavailable(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem"><h1>This content can't be read right now</h1><p>If the drive was disconnected, reconnect it and press Rescan in the library.</p><p style="color:#888;font-size:.8em">%s</p>`, html.EscapeString(err.Error()))
}

// ---- URL <-> entry mapping ----
//
// New-scheme archives keep articles in namespace C and link between them
// with paths relative to C, so we serve C entries at /zim/{pack}/{path}.
// Old-scheme archives link across namespaces ("../I/x.png"), so the
// namespace stays in the URL: /zim/{pack}/A/{path}.

func entryURLPath(a *zim.Archive, e *zim.Entry) string {
	p := e.Path
	if !a.NewNamespaces {
		p = string(e.Namespace) + "/" + p
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func lookupURLPath(a *zim.Archive, p string) (*zim.Entry, error) {
	if a.NewNamespaces {
		return a.Find('C', p)
	}
	ns, rest, ok := strings.Cut(p, "/")
	if !ok || len(ns) != 1 {
		return nil, zim.ErrNotFound
	}
	return a.Find(ns[0], rest)
}

// ---- Content server (separate origin) ----

func (s *server) contentRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /zim/{pack}/{path...}", s.handleZimContent)
	mux.HandleFunc("GET /__olib/bridge.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, bridgeJS)
	})
	return s.contentGuard(mux)
}

func (s *server) contentGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// Pack content may run its own scripts, but only from the archive,
		// may not contact anything but this content server, and may only be
		// framed by the app.
		h.Set("Content-Security-Policy", "default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; form-action 'none'; base-uri 'self'; frame-ancestors "+s.appOrigins())
		if !s.dev {
			if r.Host != s.contentHost {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			// The session cookie is scoped to the host, not the port, so the
			// browser sends it here too.
			c, err := r.Cookie(s.cookieName())
			if err != nil || !tokenEqual(c.Value, s.token) {
				http.Error(w, "Open the library from its launcher.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) appOrigins() string {
	if s.dev {
		return "http://" + s.host + " http://localhost:5173 http://127.0.0.1:5173"
	}
	return "http://" + s.host
}

func (s *server) handleZimContent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("pack")
	_, a, ok := s.zimPack(w, id)
	if !ok {
		return
	}
	base := "/zim/" + url.PathEscape(id) + "/"
	p := r.PathValue("path")

	var e *zim.Entry
	var err error
	if p == "" {
		e, err = a.MainPage()
	} else {
		e, err = lookupURLPath(a, p)
	}
	if errors.Is(err, zim.ErrNotFound) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem"><h1>Not in this archive</h1><p>This page isn't included in the offline copy.</p>`)
		return
	}
	if err != nil {
		s.archives.drop(id)
		unavailable(w, err)
		return
	}
	if p == "" || e.IsRedirect {
		target, err := a.Resolve(e)
		if err != nil {
			unavailable(w, err)
			return
		}
		http.Redirect(w, r, base+entryURLPath(a, target), http.StatusFound)
		return
	}

	body, err := a.Content(e)
	if err != nil {
		s.archives.drop(id)
		unavailable(w, err)
		return
	}
	if strings.HasPrefix(e.MimeType, "text/html") {
		body = injectBridge(body)
	}
	w.Header().Set("Content-Type", e.MimeType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

// injectBridge adds the small script that reports navigation to the app and
// routes external links through it instead of opening them silently.
func injectBridge(body []byte) []byte {
	tag := []byte(`<script src="/__olib/bridge.js"></script>`)
	if i := bytes.LastIndex(bytes.ToLower(body), []byte("</body>")); i >= 0 {
		out := make([]byte, 0, len(body)+len(tag))
		out = append(out, body[:i]...)
		out = append(out, tag...)
		return append(out, body[i:]...)
	}
	return append(body, tag...)
}

const bridgeJS = `(function () {
  if (window.parent === window) return;
  var post = function (msg) { window.parent.postMessage(msg, '*'); };
  post({ type: 'olib:navigated', path: location.pathname, title: document.title });
  document.addEventListener('click', function (ev) {
    var a = ev.target && ev.target.closest ? ev.target.closest('a[href]') : null;
    if (!a) return;
    var url;
    try { url = new URL(a.getAttribute('href'), location.href); } catch (e) { return; }
    if (url.origin === location.origin) return;
    ev.preventDefault();
    if (url.protocol === 'http:' || url.protocol === 'https:') {
      post({ type: 'olib:external-link', url: url.href, text: (a.textContent || '').trim().slice(0, 200) });
    }
  }, true);
})();
`

// ---- App API for ZIM packs ----

type zimInfo struct {
	Title     string `json:"title"`
	MainPath  string `json:"mainPath"`
	Language  string `json:"language,omitempty"`
	Date      string `json:"date,omitempty"`
	Entries   uint32 `json:"entries"`
	OldFormat bool   `json:"oldFormat"`
}

func (s *server) handleZimInfo(w http.ResponseWriter, r *http.Request) {
	_, a, ok := s.zimPack(w, r.PathValue("id"))
	if !ok {
		return
	}
	info := zimInfo{Entries: a.Header.EntryCount, OldFormat: !a.NewNamespaces}
	info.Title, _ = a.Metadata("Title")
	info.Language, _ = a.Metadata("Language")
	info.Date, _ = a.Metadata("Date")
	if mp, err := a.MainPage(); err == nil {
		info.MainPath = entryURLPath(a, mp)
	}
	writeJSON(w, info)
}

type searchResult struct {
	Title string `json:"title"`
	Path  string `json:"path"` // URL path under /zim/{pack}/, already escaped
}

func (s *server) handleZimSearch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, a, ok := s.zimPack(w, id)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	results := []searchResult{}
	if q == "" {
		writeJSON(w, results)
		return
	}

	// Titles are stored case-sensitively, so try the query as typed plus
	// the common capitalizations of article titles.
	seen := map[string]bool{}
	for _, variant := range titleVariants(q) {
		matches, err := a.SearchTitles(variant, limit)
		if err != nil {
			s.archives.drop(id)
			unavailable(w, err)
			return
		}
		for _, m := range matches {
			if seen[m.Path] || len(results) >= limit {
				continue
			}
			seen[m.Path] = true
			e := &zim.Entry{Namespace: a.ContentNamespace(), Path: m.Path}
			results = append(results, searchResult{Title: m.Title, Path: entryURLPath(a, e)})
		}
	}
	writeJSON(w, results)
}

func titleVariants(q string) []string {
	out := []string{q}
	add := func(v string) {
		for _, o := range out {
			if o == v {
				return
			}
		}
		out = append(out, v)
	}
	add(upperFirst(q))
	words := strings.Fields(q)
	for i, w := range words {
		words[i] = upperFirst(strings.ToLower(w))
	}
	add(strings.Join(words, " "))
	add(upperFirst(strings.ToLower(q)))
	return out
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
