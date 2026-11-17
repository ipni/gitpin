// Package site assembles the static gh-pages site layout for a gitpin run.
package site

import (
	"errors"

	"github.com/ipni/gitpin/internal/dag"
	"github.com/ipni/gitpin/internal/state"
	"github.com/multiformats/go-multiaddr"
)

var errNotImplemented = errors.New("not implemented")

// ProbeCID is the CID of the empty payload used to probe the gateway.
const ProbeCID = "bafkqaaa"

// BaseAddr converts a site base URL into a libp2p HTTP gateway multiaddr.
func BaseAddr(baseURL string) (multiaddr.Multiaddr, error) {
	return nil, errNotImplemented
}

// Assemble writes the static site files for the given state and roots.
func Assemble(siteDir string, st state.State, roots []dag.Root) error {
	return errNotImplemented
}
