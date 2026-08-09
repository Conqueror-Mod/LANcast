// Command lcsign signs release artifacts with the project's release key.
//
// It signs one file — the checksums file goreleaser already produces — rather
// than every artifact. One signature over a list of digests covers the whole
// release, and it is the only part a verifier needs to trust: match an artifact
// against its digest in a signed list and the artifact is as good as signed.
//
// Deliberately separate from lcplugin's key. Plugin provenance and release
// provenance are different trust domains, and one key for both means a
// compromise of either is a compromise of everything. They share an algorithm
// and nothing else.
//
//	lcsign keygen -out release.key
//	lcsign sign   -key release.key -in checksums.txt -out checksums.txt.sig
//	lcsign verify -pub <hex> -in checksums.txt -sig checksums.txt.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcsign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  lcsign keygen -out <keyfile>")
	fmt.Fprintln(os.Stderr, "  lcsign sign   -key <keyfile> -in <file> -out <file.sig>")
	fmt.Fprintln(os.Stderr, "  lcsign verify -pub <hex> -in <file> -sig <file.sig>")
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "release.key", "where to write the private key")
	_ = fs.Parse(args)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	// 0600, and the caller is told to keep it out of the repo. *.key is already
	// gitignored for exactly this reason (ADR 0021).
	if err := os.WriteFile(*out, []byte(hex.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("private key: %s (keep this out of the repository)\n", *out)
	fmt.Printf("public key:  %s\n", hex.EncodeToString(pub))
	fmt.Println()
	fmt.Println("Put the public half in internal/release/publickey.go, and the")
	fmt.Println("private half in the RELEASE_SIGNING_KEY repository secret.")
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyPath := fs.String("key", "", "private key file, or - to read from RELEASE_SIGNING_KEY")
	in := fs.String("in", "", "file to sign")
	out := fs.String("out", "", "signature file to write")
	_ = fs.Parse(args)

	if *in == "" || *out == "" {
		return fmt.Errorf("sign needs -in and -out")
	}

	priv, err := loadKey(*keyPath)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(priv, body)
	return os.WriteFile(*out, []byte(hex.EncodeToString(sig)+"\n"), 0o644)
}

// loadKey reads the private key from a file, or from the environment when the
// path is empty or "-". CI holds it as a secret and never writes it to disk,
// which is one fewer place it can be left behind.
func loadKey(path string) (ed25519.PrivateKey, error) {
	var raw string
	if path == "" || path == "-" {
		raw = os.Getenv("RELEASE_SIGNING_KEY")
		if raw == "" {
			return nil, fmt.Errorf("no -key given and RELEASE_SIGNING_KEY is empty")
		}
	} else {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw = string(b)
	}
	b, err := hex.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("key is not hex: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("key is %d bytes, want %d", len(b), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(b), nil
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pub := fs.String("pub", "", "public key, hex")
	in := fs.String("in", "", "file that was signed")
	sigPath := fs.String("sig", "", "signature file")
	_ = fs.Parse(args)

	pb, err := hex.DecodeString(strings.TrimSpace(*pub))
	if err != nil || len(pb) != ed25519.PublicKeySize {
		return fmt.Errorf("-pub must be a hex ed25519 public key")
	}
	body, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	sigHex, err := os.ReadFile(*sigPath)
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(strings.TrimSpace(string(sigHex)))
	if err != nil {
		return fmt.Errorf("signature is not hex: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pb), body, sig) {
		return fmt.Errorf("signature does not verify")
	}
	fmt.Println("ok")
	return nil
}
