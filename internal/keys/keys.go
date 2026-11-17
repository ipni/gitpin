// Package keys handles the gitpin signing key.
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
)

var (
	errEmpty  = errors.New("GITPIN_KEY is empty")
	errBadKey = errors.New("GITPIN_KEY is not an Ed25519 libp2p private key")
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

// Parse decodes a base64-encoded signing key.
func Parse(s string) (crypto.PrivKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errEmpty
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

// FromEnv loads the signing key from the GITPIN_KEY environment variable.
func FromEnv() (crypto.PrivKey, error) {
	return Parse(os.Getenv(EnvVar))
}
