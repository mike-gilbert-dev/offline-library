// Command releasesign creates the release signing key and signs release
// manifests. The private key never belongs in the repository.
//
//	releasesign keygen -key <path>                  create a key pair (prints the public key)
//	releasesign pubkey -key <path>                  print the public key again
//	releasesign sign   -key <path> -in release.json writes release.json.sig
//	releasesign verify -pub <base64> -in release.json
//
// Keys are Ed25519. The key file holds the 32-byte seed, base64-encoded,
// after a comment line; signatures are base64-encoded on a single line.
package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	keyPath := fs.String("key", "", "path to the private key file")
	in := fs.String("in", "", "file to sign or verify")
	pub := fs.String("pub", "", "public key (base64) for verify")
	_ = fs.Parse(args)

	var err error
	switch cmd {
	case "keygen":
		err = keygen(*keyPath)
	case "pubkey":
		var priv ed25519.PrivateKey
		if priv, err = loadKey(*keyPath); err == nil {
			fmt.Println(base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)))
		}
	case "sign":
		err = sign(*keyPath, *in)
	case "verify":
		err = verify(*pub, *in)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "releasesign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: releasesign keygen|pubkey|sign|verify [flags]")
	os.Exit(2)
}

func keygen(path string) error {
	if path == "" {
		return errors.New("-key is required")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a signing key", path)
	}
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content := "# Offline Library release signing key (Ed25519 seed). Keep private; back it up.\n" +
		base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"
	// O_EXCL: never clobber an existing key, even in a race.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pubKey))
	return nil
}

func loadKey(path string) (ed25519.PrivateKey, error) {
	if path == "" {
		return nil, errors.New("-key is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seed, err := base64.StdEncoding.DecodeString(line)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, errors.New("key file is not a valid signing key")
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	return nil, errors.New("key file is empty")
}

func sign(keyPath, in string) error {
	priv, err := loadKey(keyPath)
	if err != nil {
		return err
	}
	if in == "" {
		return errors.New("-in is required")
	}
	msg, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, msg)
	return os.WriteFile(in+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}

func verify(pubB64, in string) error {
	pubKey, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pubKey) != ed25519.PublicKeySize {
		return errors.New("-pub is not a valid public key")
	}
	msg, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	sigText, err := os.ReadFile(in + ".sig")
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || !ed25519.Verify(pubKey, msg, sig) {
		return errors.New("signature does NOT match")
	}
	fmt.Println("signature OK")
	return nil
}
