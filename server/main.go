// Command server is the Offline Library launcher. It finds the OfflineLibrary
// folder next to itself, serves the bundled UI and pack content on
// 127.0.0.1 only, and opens the user's default browser.
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

//go:embed all:webdist
var webFS embed.FS

type server struct {
	mu   sync.RWMutex
	root string
	lib  *Library

	token       string // required (via cookie) on every request outside dev mode
	host        string // expected Host header for the app, e.g. "127.0.0.1:53211"
	contentHost string // pack content is served from this second origin
	dev         bool

	archives archiveSet
}

func main() {
	libFlag := flag.String("library", "", "path to the OfflineLibrary folder (default: the one next to this program)")
	port := flag.Int("port", 0, "port to listen on (default: any free port)")
	noBrowser := flag.Bool("no-browser", false, "don't open a browser window")
	dev := flag.Bool("dev", false, "development mode: port 8787, no access token, for use with the Vite dev server")
	flag.Parse()

	root, err := resolveLibraryRoot(*libFlag)
	if err != nil {
		log.Fatal(err)
	}
	if *dev && *port == 0 {
		*port = 8787
	}

	// Loopback only: the library is never reachable from the network.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatalf("could not start the local server: %v", err)
	}

	// Pack content (e.g. Wikipedia pages and their scripts) gets its own
	// origin on a second port, so it can't call the app's API or read its pages.
	contentPort := 0
	if *dev {
		contentPort = *port + 1
	}
	cln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", contentPort))
	if err != nil {
		log.Fatalf("could not start the local content server: %v", err)
	}

	s := &server{root: root, token: randomToken(), host: ln.Addr().String(), contentHost: cln.Addr().String(), dev: *dev}
	s.lib = Scan(root)

	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	csrv := &http.Server{Handler: s.contentRoutes(), ReadHeaderTimeout: 10 * time.Second}
	for _, pair := range []struct {
		srv *http.Server
		ln  net.Listener
	}{{srv, ln}, {csrv, cln}} {
		go func() {
			if err := pair.srv.Serve(pair.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal(err)
			}
		}()
	}

	base := "http://" + s.host + "/"
	openURL := base + "auth?token=" + s.token
	if *dev {
		openURL = base
	}

	fmt.Println("Offline Library")
	fmt.Println("  Library:", root)
	fmt.Printf("  Found %d pack(s)%s\n", len(s.lib.Packs), map[bool]string{true: "", false: " (library not available)"}[s.lib.Available])
	fmt.Println("  Address:", openURL)
	fmt.Println()
	fmt.Println("Keep this window open while you use the library. Close it (or press Ctrl+C) to stop.")

	if !*noBrowser && !*dev {
		if err := openBrowser(openURL); err != nil {
			fmt.Println("Could not open a browser automatically. Paste the address above into your browser.")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = csrv.Shutdown(shutdownCtx)
	s.archives.closeAll()
}

// resolveLibraryRoot picks the library folder: the -library flag, else an
// OfflineLibrary folder next to the executable, else one in the current
// directory. If none has a library.json, it still returns the first candidate
// so the UI can explain what it was looking for.
func resolveLibraryRoot(flagVal string) (string, error) {
	if flagVal != "" {
		return filepath.Abs(flagVal)
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "OfflineLibrary"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "OfflineLibrary"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "library.json")); err == nil {
			return c, nil
		}
	}
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return "", errors.New("could not work out where to look for the library; use -library")
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth", s.handleAuth)
	mux.HandleFunc("GET /api/library", s.handleLibrary)
	mux.HandleFunc("POST /api/rescan", s.handleRescan)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/packs/{id}/zim", s.handleZimInfo)
	mux.HandleFunc("GET /api/packs/{id}/search", s.handleZimSearch)
	mux.HandleFunc("GET /content/{pack}/{path...}", s.handleContent)

	ui, err := fs.Sub(webFS, "webdist")
	if err != nil {
		log.Fatal(err)
	}
	files := http.FileServer(http.FS(ui))
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; frame-src http://"+s.contentHost)
		files.ServeHTTP(w, r)
	}))
	return s.guard(mux)
}

func (s *server) cookieName() string { return "olib_session" }

// guard enforces the Host check (blocks DNS-rebinding attacks from web pages)
// and the session cookie (blocks other local programs and pages).
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if s.dev {
			next.ServeHTTP(w, r)
			return
		}
		if r.Host != s.host {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+s.host {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		if r.URL.Path != "/auth" {
			c, err := r.Cookie(s.cookieName())
			if err != nil || !tokenEqual(c.Value, s.token) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>Offline Library</title><body style="font-family:system-ui;max-width:36rem;margin:4rem auto;padding:0 1rem"><h1>Open the library from its launcher</h1><p>This page needs to be opened by the launcher on the drive (for example <code>start-windows.exe</code>). Run it again and it will open a new browser window.</p>`)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if !tokenEqual(r.URL.Query().Get("token"), s.token) {
		http.Error(w, "This link has expired. Run the launcher again.", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: s.token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, s.lib)
}

func (s *server) handleRescan(w http.ResponseWriter, r *http.Request) {
	lib := Scan(s.root)
	s.mu.Lock()
	s.lib = lib
	s.mu.Unlock()
	// Reopen archives on next use: the drive may have been swapped or reconnected.
	s.archives.closeAll()
	writeJSON(w, lib)
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"contentOrigin": "http://" + s.contentHost})
}

// handleStatus is a cheap availability check the UI polls to notice the
// drive being unplugged or reconnected.
func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(filepath.Join(s.root, "library.json"))
	writeJSON(w, map[string]bool{"available": err == nil})
}

// handleContent serves only files that the pack manifest declares and the
// last scan verified. Range requests are supported via http.ServeContent.
func (s *server) handleContent(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	pack := s.lib.FindPack(r.PathValue("pack"))
	var abs string
	ok := false
	if pack != nil && pack.Status == StatusOK {
		abs, ok = pack.files[r.PathValue("path")]
	}
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(abs)
	if err != nil {
		http.Error(w, "This file can't be read right now. If the drive was disconnected, reconnect it and press Rescan.", http.StatusServiceUnavailable)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "This file can't be read right now.", http.StatusServiceUnavailable)
		return
	}
	// TODO: when HTML/ZIM packs arrive, serve them from a separate origin
	// with a sandboxing CSP so pack content can't reach the app's API.
	http.ServeContent(w, r, filepath.Base(abs), st.ModTime(), f)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func tokenEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
