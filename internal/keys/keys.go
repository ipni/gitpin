// Package keys handles the gitpin signing key. A key is stored either as the
// base64 output of gitpin keygen or as a PEM private key written by ssh-keygen
// (or openssl), both Ed25519.
package keys

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p/core/crypto"
	"golang.org/x/crypto/ssh"
)

var (
	errEmpty      = errors.New("GITPIN_KEY is empty")
	errBadKey     = errors.New("GITPIN_KEY is not an Ed25519 key (expected an ssh-keygen -t ed25519 private key or gitpin keygen output)")
	errPassphrase = errors.New("GITPIN_KEY has a passphrase; make a new key and press Enter at the passphrase prompt")
)

// EnvVar is the environment variable holding the base64-encoded signing key.
const EnvVar = "GITPIN_KEY"

// Generate creates a new signing key and its base64 encoding.
func Generate() (crypto.PrivKey, string, error) {
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	raw, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, "", err
	}
	return priv, base64.StdEncoding.EncodeToString(raw), nil
}

// Parse decodes a signing key given either as the base64 output of gitpin
// keygen or as a PEM private key from ssh-keygen (or openssl).
func Parse(s string) (crypto.PrivKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errEmpty
	}
	if strings.HasPrefix(s, "-----BEGIN") {
		return parsePEM(s)
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errBadKey
	}
	k, err := crypto.UnmarshalPrivateKey(raw)
	if err != nil || k.Type() != crypto.Ed25519 {
		return nil, errBadKey
	}
	buf, err := k.Raw()
	if err != nil || len(buf) != 64 || !bytes.Equal(buf[32:], ed25519.NewKeyFromSeed(buf[:32]).Public().(ed25519.PublicKey)) {
		return nil, errBadKey
	}
	return k, nil
}

// parsePEM decodes a PEM private key (OpenSSH or PKCS#8) into a libp2p Ed25519 key.
func parsePEM(s string) (crypto.PrivKey, error) {
	raw, err := ssh.ParseRawPrivateKey([]byte(s))
	if err != nil {
		var pme *ssh.PassphraseMissingError
		if errors.As(err, &pme) {
			return nil, errPassphrase
		}
		return nil, errBadKey
	}
	var edKey ed25519.PrivateKey
	switch k := raw.(type) {
	case *ed25519.PrivateKey:
		edKey = *k
	case ed25519.PrivateKey:
		edKey = k
	default:
		return nil, errBadKey
	}
	k, err := crypto.UnmarshalEd25519PrivateKey([]byte(edKey))
	if err != nil {
		return nil, errBadKey
	}
	return k, nil
}

// FromEnv loads the signing key from the GITPIN_KEY environment variable.
func FromEnv() (crypto.PrivKey, error) {
	return Parse(os.Getenv(EnvVar))
}
