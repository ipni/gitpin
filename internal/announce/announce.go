// Package announce waits for content to be served and announces the chain head to indexers.
package announce

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var errNotImplemented = errors.New("not implemented")

// WaitServed polls the gateway until the want bytes are served or ctx ends.
func WaitServed(ctx context.Context, c *http.Client, baseURL string, want []byte, every time.Duration) error {
	return errNotImplemented
}

// Send announces the chain head to an indexer.
func Send(ctx context.Context, indexerURL string, id peer.ID, head cid.Cid, addr multiaddr.Multiaddr) error {
	return errNotImplemented
}

// WaitIndexed polls an indexer until root is indexed or ctx ends.
func WaitIndexed(ctx context.Context, c *http.Client, indexerURL string, root cid.Cid, id peer.ID, every time.Duration) error {
	return errNotImplemented
}
