// Package ipni publishes the IPNI advertisement chain for the published roots.
package ipni

import (
	"errors"

	"github.com/ipni/gitpin/internal/dag"
	"github.com/ipni/gitpin/internal/state"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/multiformats/go-multiaddr"
)

var errNotImplemented = errors.New("not implemented")

// Topic is the IPNI ingest topic for the mainnet.
const Topic = "/indexer/ingest/mainnet"

// EntriesPerChunk is the number of entries packed into each entry chunk.
const EntriesPerChunk = 16384

// Params configures an advertisement publish run.
type Params struct {
	Key     crypto.PrivKey
	Addr    multiaddr.Multiaddr
	BaseURL string
	AdDir   string
}

// Publish writes new advertisements for roots and returns the next state and whether it changed.
func Publish(p Params, prev state.State, roots []dag.Root) (state.State, bool, error) {
	return state.State{}, false, errNotImplemented
}
