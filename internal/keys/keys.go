// Package keys handles the gitpin signing key.
package keys

import (
	"errors"

	"github.com/libp2p/go-libp2p/core/crypto"
)

var errNotImplemented = errors.New("not implemented")

// EnvVar is the environment variable holding the base64-encoded signing key.
const EnvVar = "GITPIN_KEY"

// Generate creates a new signing key and its base64 encoding.
func Generate() (crypto.PrivKey, string, error) {
	return nil, "", errNotImplemented
}

// Parse decodes a base64-encoded signing key.
func Parse(s string) (crypto.PrivKey, error) {
	return nil, errNotImplemented
}

// FromEnv loads the signing key from the GITPIN_KEY environment variable.
func FromEnv() (crypto.PrivKey, error) {
	return nil, errNotImplemented
}
