package main

// Release manifests and their signatures. Every release publishes
// release.json (the version plus the SHA-256 and size of each launcher) and
// release.json.sig, an Ed25519 signature made with a private key that never
// leaves the maintainer's computer (see cmd/releasesign). The updater trusts a
// release only if its manifest verifies against the public key below, and
// then only files whose checksum and size match the manifest.
//
// The version is inside the signed manifest, so an older release can't be
// passed off as a newer one.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// releasePublicKey verifies release manifests. It is a variable only so tests
// can substitute their own key pair.
var releasePublicKey = mustPublicKey("+hRrMef3CWzhuEtrC/iNbxUECXMjjVZQfsmEbN62+qE=")

type releaseFile struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type releaseManifest struct {
	Schema  int                    `json:"schema"`
	Version string                 `json:"version"`
	Files   map[string]releaseFile `json:"files"`
}

var (
	sha256Hex       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	releaseFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)
)

// verifyReleaseManifest checks the signature over the exact manifest bytes,
// then parses and sanity-checks the manifest. wantVersion, if not empty, must
// match the version inside the manifest (the release tag being installed).
func verifyReleaseManifest(manifest, sigText []byte, wantVersion string) (*releaseManifest, error) {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("the release signature is malformed")
	}
	if !ed25519.Verify(releasePublicKey, manifest, sig) {
		return nil, errors.New("the release signature doesn't match; this update was not published by the Offline Library maintainer")
	}

	var m releaseManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, fmt.Errorf("the release manifest can't be read: %w", err)
	}
	if m.Schema != 1 {
		return nil, fmt.Errorf("this release needs a newer updater (manifest schema %d)", m.Schema)
	}
	if parseVersion(m.Version) == nil {
		return nil, errors.New("the release manifest has no valid version")
	}
	if wantVersion != "" && m.Version != wantVersion {
		return nil, fmt.Errorf("the release manifest is for %s, not %s", m.Version, wantVersion)
	}
	if len(m.Files) == 0 {
		return nil, errors.New("the release manifest lists no files")
	}
	for name, f := range m.Files {
		// Names become paths on the drive, so allow plain file names only.
		if !releaseFileName.MatchString(name) || strings.Contains(name, "..") {
			return nil, fmt.Errorf("the release manifest has an unsafe file name %q", name)
		}
		if !sha256Hex.MatchString(f.SHA256) || f.Size <= 0 {
			return nil, fmt.Errorf("the release manifest entry for %q is invalid", name)
		}
	}
	return &m, nil
}

func mustPublicKey(b64 string) ed25519.PublicKey {
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(k) != ed25519.PublicKeySize {
		panic("invalid release public key")
	}
	return ed25519.PublicKey(k)
}
