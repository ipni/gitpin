// Package dag builds UnixFS DAGs for the files in a content directory.
package dag

import (
	"errors"

	"github.com/ipfs/go-cid"
)

var errNotImplemented = errors.New("not implemented")

// Chunker names the chunking strategy used when building file DAGs.
const Chunker = "size-262144"

// MaxTotalBytes is the maximum total content size a build may process.
const MaxTotalBytes = 500 << 20

// MaxBlocks is the maximum number of blocks a single build may produce.
const MaxBlocks = 50000

// Root is a built content root and the blocks that back it.
type Root struct {
	Name   string
	CID    cid.Cid
	Blocks []cid.Cid
}

// Build builds a UnixFS DAG for each file in contentDir, writing blocks to blocksDir.
func Build(contentDir, blocksDir string) ([]Root, error) {
	return nil, errNotImplemented
}
