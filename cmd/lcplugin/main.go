// Command lcplugin builds and signs LANcast plugin bundles (.lcplugin), ADR 0021.
//
//	lcplugin keygen -out project
//	    → writes project.key (private, 0600) and prints the public key hex to
//	      embed in internal/plugin/projectkey.go. Guard the .key file; it never
//	      belongs in the repo.
//
//	lcplugin sign -in plugins/omdb -out omdb.lcplugin [-key project.key]
//	    → packs plugin.json + plugin.wasm from -in into a bundle. With -key it is
//	      signed; without, it is an unsigned bundle (the installer names it so).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/plugin"
)

func main() {
	if len(os.Args) < 2 {
		usage()
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
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcplugin:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  lcplugin keygen -out <name>")
	fmt.Fprintln(os.Stderr, "  lcplugin sign -in <dir> -out <bundle.lcplugin> [-key <keyfile>]")
	fmt.Fprintln(os.Stderr, "  lcplugin verify -in <bundle.lcplugin> [-key <public-key-hex>]")
	os.Exit(2)
}

// flags is a tiny -k v parser, enough for this tool without pulling in the flag
// package's per-subcommand ceremony.
func flags(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i+1 < len(args); i += 2 {
		out[strings.TrimPrefix(args[i], "-")] = args[i+1]
	}
	return out
}

func keygen(args []string) error {
	f := flags(args)
	name := f["out"]
	if name == "" {
		return fmt.Errorf("keygen needs -out <name>")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	keyFile := name + ".key"
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(priv)), 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s (private key — guard it, keep it out of the repo)\n", keyFile)
	fmt.Printf("public key (embed in internal/plugin/projectkey.go):\n%s\n", hex.EncodeToString(pub))
	return nil
}

func verify(args []string) error {
	f := flags(args)
	in := f["in"]
	if in == "" {
		return fmt.Errorf("verify needs -in <bundle>")
	}
	bundle, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	keys := plugin.TrustedKeys{Project: plugin.ProjectPublicKey()}
	if kh := f["key"]; kh != "" {
		b, err := hex.DecodeString(strings.TrimSpace(kh))
		if err != nil || len(b) != ed25519.PublicKeySize {
			return fmt.Errorf("-key must be a hex ed25519 public key")
		}
		keys.Pinned = append(keys.Pinned, ed25519.PublicKey(b))
	}
	vb, err := plugin.VerifyBundle(bundle, keys)
	if err != nil {
		return err
	}
	fmt.Printf("ok: %s v%s (%s), signer=%s\n  digest %s\n  capabilities: http=%v secrets=%v\n",
		vb.Manifest.Name, vb.Manifest.Version, vb.Manifest.Kind, vb.Signer, vb.Digest,
		vb.Manifest.Capabilities.HTTP, vb.Manifest.Capabilities.Secrets)
	return nil
}

func sign(args []string) error {
	f := flags(args)
	in, out := f["in"], f["out"]
	if in == "" || out == "" {
		return fmt.Errorf("sign needs -in <dir> and -out <bundle>")
	}
	manifest, err := os.ReadFile(filepath.Join(in, "plugin.json"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	wasm, err := os.ReadFile(filepath.Join(in, "plugin.wasm"))
	if err != nil {
		return fmt.Errorf("read module: %w", err)
	}

	var priv ed25519.PrivateKey
	if kf := f["key"]; kf != "" {
		raw, err := os.ReadFile(kf)
		if err != nil {
			return fmt.Errorf("read key: %w", err)
		}
		b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(b) != ed25519.PrivateKeySize {
			return fmt.Errorf("key file is not a valid ed25519 private key")
		}
		priv = ed25519.PrivateKey(b)
	}

	bundle, err := plugin.CreateBundle(manifest, wasm, priv)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, bundle, 0o644); err != nil {
		return err
	}
	kind := "unsigned"
	if priv != nil {
		kind = "signed"
	}
	fmt.Printf("wrote %s (%s, %d bytes)\n", out, kind, len(bundle))
	return nil
}
