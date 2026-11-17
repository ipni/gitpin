// Package deps pins the modules the other packages use so go.mod stays stable while they are written in parallel.
package deps

import (
	_ "github.com/ipfs/go-cid"
	_ "github.com/ipfs/go-unixfsnode"
	_ "github.com/ipfs/go-unixfsnode/data/builder"
	_ "github.com/ipfs/go-unixfsnode/file"
	_ "github.com/ipld/go-codec-dagpb"
	_ "github.com/ipld/go-ipld-prime"
	_ "github.com/ipld/go-ipld-prime/codec/dagjson"
	_ "github.com/ipld/go-ipld-prime/linking/cid"
	_ "github.com/ipld/go-ipld-prime/node/basicnode"
	_ "github.com/ipld/go-ipld-prime/storage/memstore"
	_ "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	_ "github.com/ipni/go-libipni/announce/httpsender"
	_ "github.com/ipni/go-libipni/announce/message"
	_ "github.com/ipni/go-libipni/dagsync/ipnisync"
	_ "github.com/ipni/go-libipni/dagsync/ipnisync/head"
	_ "github.com/ipni/go-libipni/ingest/schema"
	_ "github.com/ipni/go-libipni/metadata"
	_ "github.com/libp2p/go-libp2p/core/crypto"
	_ "github.com/libp2p/go-libp2p/core/peer"
	_ "github.com/multiformats/go-multiaddr"
	_ "github.com/multiformats/go-multicodec"
	_ "github.com/multiformats/go-multihash"
)
