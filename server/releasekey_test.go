package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// withTestKey swaps in a throwaway key pair and returns a signer.
func withTestKey(t *testing.T) func([]byte) []byte {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := releasePublicKey
	releasePublicKey = pub
	t.Cleanup(func() { releasePublicKey = old })
	return func(msg []byte) []byte {
		return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg)) + "\n")
	}
}

const goodManifest = `{"schema":1,"version":"v0.3.0","files":{
  "start-windows.exe":{"sha256":"7d55bf6e65390091359aa2f9d4f5b59bb066376fcdf4265c49d4db46e8b779c6","size":10000000},
  "Start Offline Library.command":{"sha256":"a2bf66b659367020b7461f06b1a1ad22fa2702b33728b604fa147adfacfa4425","size":300}}}`

func TestVerifyReleaseManifest(t *testing.T) {
	sign := withTestKey(t)
	m, err := verifyReleaseManifest([]byte(goodManifest), sign([]byte(goodManifest)), "v0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "v0.3.0" || len(m.Files) != 2 || m.Files["start-windows.exe"].Size != 10000000 {
		t.Fatalf("parsed wrong: %+v", m)
	}
}

func TestVerifyReleaseManifestRejects(t *testing.T) {
	sign := withTestKey(t)
	good := []byte(goodManifest)

	tampered := []byte(strings.Replace(goodManifest, "10000000", "10000001", 1))
	if _, err := verifyReleaseManifest(tampered, sign(good), ""); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Errorf("tampered manifest accepted: %v", err)
	}
	if _, err := verifyReleaseManifest(good, []byte("not base64!"), ""); err == nil {
		t.Error("malformed signature accepted")
	}
	if _, err := verifyReleaseManifest(good, sign(good), "v0.4.0"); err == nil || !strings.Contains(err.Error(), "not v0.4.0") {
		t.Errorf("version mismatch accepted: %v", err)
	}

	// Signed with a different key (e.g. someone else's release).
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	otherSig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(otherPriv, good)))
	if _, err := verifyReleaseManifest(good, otherSig, ""); err == nil {
		t.Error("manifest signed by another key accepted")
	}

	// Validly signed but unsafe or invalid contents.
	for name, manifest := range map[string]string{
		"path traversal": `{"schema":1,"version":"v0.3.0","files":{"../evil.exe":{"sha256":"` + strings.Repeat("a", 64) + `","size":1}}}`,
		"subdirectory":   `{"schema":1,"version":"v0.3.0","files":{"OfflineLibrary/library.json":{"sha256":"` + strings.Repeat("a", 64) + `","size":1}}}`,
		"bad checksum":   `{"schema":1,"version":"v0.3.0","files":{"start-linux":{"sha256":"xyz","size":1}}}`,
		"no files":       `{"schema":1,"version":"v0.3.0","files":{}}`,
		"future schema":  `{"schema":2,"version":"v0.3.0","files":{}}`,
		"bad version":    `{"schema":1,"version":"latest","files":{}}`,
	} {
		if _, err := verifyReleaseManifest([]byte(manifest), sign([]byte(manifest)), ""); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEmbeddedPublicKeyIsValid(t *testing.T) {
	if len(releasePublicKey) != ed25519.PublicKeySize {
		t.Fatal("embedded release public key has the wrong size")
	}
}
