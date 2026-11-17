// Package ipni publishes the IPNI advertisement chain for the published roots.
package ipni

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/gitpin/internal/dag"
	"github.com/ipni/gitpin/internal/state"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
)

// Topic is the IPNI ingest topic for the mainnet.
const Topic = "/indexer/ingest/mainnet"

// EntriesPerChunk is the number of entries packed into each entry chunk.
const EntriesPerChunk = 16384

// entriesPerChunk is the chunk size used at runtime; tests lower it.
var entriesPerChunk = EntriesPerChunk

// Params configures an advertisement publish run.
type Params struct {
	Key     crypto.PrivKey
	Addr    multiaddr.Multiaddr
	BaseURL string
	AdDir   string
}

// Publish writes new advertisements for roots and returns the next state and whether it changed.
func Publish(p Params, prev state.State, roots []dag.Root) (state.State, bool, error) {
	id, err := peer.IDFromPrivateKey(p.Key)
	if err != nil {
		return state.State{}, false, fmt.Errorf("peer id: %w", err)
	}
	if prev.PeerID != "" && prev.PeerID != id.String() {
		return state.State{}, false, fmt.Errorf("GITPIN_KEY belongs to %s, but gitpin.json was written by %s; to start over with a new key, delete gitpin.json and ipni/ from the gh-pages branch", id, prev.PeerID)
	}

	var headCid cid.Cid
	if prev.Head != "" {
		headCid, err = cid.Parse(prev.Head)
		if err != nil {
			return state.State{}, false, fmt.Errorf("previous head %q: %w", prev.Head, err)
		}
	}

	oldSet := make(map[string]cid.Cid, len(prev.Roots))
	prevAd := make(map[string]string, len(prev.Roots))
	for _, r := range prev.Roots {
		c, err := cid.Parse(r.CID)
		if err != nil {
			return state.State{}, false, fmt.Errorf("previous root %q: %w", r.CID, err)
		}
		if _, ok := oldSet[r.CID]; !ok {
			oldSet[r.CID] = c
			prevAd[r.CID] = r.Ad
		}
	}
	newSet := make(map[string]struct{}, len(roots))
	for _, r := range roots {
		if len(r.Blocks) == 0 {
			return state.State{}, false, fmt.Errorf("root %s has no blocks", r.Name)
		}
		newSet[r.CID.String()] = struct{}{}
	}

	ls := cidlink.DefaultLinkSystem()
	ls.StorageWriteOpener = func(ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		var buf bytes.Buffer
		return &buf, func(lnk ipld.Link) error {
			cl, ok := lnk.(cidlink.Link)
			if !ok {
				return fmt.Errorf("unexpected link type %T", lnk)
			}
			if err := os.MkdirAll(p.AdDir, 0o755); err != nil {
				return err
			}
			return writeFile(filepath.Join(p.AdDir, cl.Cid.String()), buf.Bytes())
		}, nil
	}
	store := func(node ipld.Node) (cid.Cid, error) {
		lnk, err := ls.Store(ipld.LinkContext{}, schema.Linkproto, node)
		if err != nil {
			return cid.Undef, err
		}
		return lnk.(cidlink.Link).Cid, nil
	}

	changed := false
	appendAd := func(ad schema.Advertisement) (cid.Cid, error) {
		if headCid.Defined() {
			ad.PreviousID = cidlink.Link{Cid: headCid}
		}
		ad.Provider = id.String()
		ad.Addresses = []string{p.Addr.String()}
		if err := ad.Sign(p.Key); err != nil {
			return cid.Undef, fmt.Errorf("sign ad: %w", err)
		}
		if err := ad.Validate(); err != nil {
			return cid.Undef, fmt.Errorf("validate ad: %w", err)
		}
		node, err := ad.ToNode()
		if err != nil {
			return cid.Undef, fmt.Errorf("ad node: %w", err)
		}
		adCid, err := store(node)
		if err != nil {
			return cid.Undef, fmt.Errorf("store ad: %w", err)
		}
		headCid = adCid
		changed = true
		return adCid, nil
	}

	removed := make([]string, 0, len(oldSet))
	for c := range oldSet {
		if _, ok := newSet[c]; !ok {
			removed = append(removed, c)
		}
	}
	sort.Strings(removed)
	for _, c := range removed {
		if _, err := appendAd(schema.Advertisement{ContextID: oldSet[c].Bytes(), IsRm: true, Entries: schema.NoEntries}); err != nil {
			return state.State{}, false, err
		}
	}

	md := metadata.Default.New(metadata.IpfsGatewayHttp{})
	mdBytes, err := md.MarshalBinary()
	if err != nil {
		return state.State{}, false, fmt.Errorf("metadata: %w", err)
	}

	addedAd := make(map[string]string)
	for _, r := range roots {
		cidStr := r.CID.String()
		if _, ok := oldSet[cidStr]; ok {
			continue
		}
		if _, ok := addedAd[cidStr]; ok {
			continue
		}
		entries := make([]multihash.Multihash, 0, len(r.Blocks))
		for _, b := range r.Blocks {
			entries = append(entries, b.Hash())
		}
		var chunks [][]multihash.Multihash
		for i := 0; i < len(entries); i += entriesPerChunk {
			end := i + entriesPerChunk
			if end > len(entries) {
				end = len(entries)
			}
			chunks = append(chunks, entries[i:end])
		}
		var next ipld.Link
		for i := len(chunks) - 1; i >= 0; i-- {
			node, err := (schema.EntryChunk{Entries: chunks[i], Next: next}).ToNode()
			if err != nil {
				return state.State{}, false, fmt.Errorf("entry chunk: %w", err)
			}
			chunkCid, err := store(node)
			if err != nil {
				return state.State{}, false, fmt.Errorf("store entry chunk: %w", err)
			}
			next = cidlink.Link{Cid: chunkCid}
		}
		adCid, err := appendAd(schema.Advertisement{ContextID: r.CID.Bytes(), Metadata: mdBytes, Entries: next})
		if err != nil {
			return state.State{}, false, err
		}
		addedAd[cidStr] = adCid.String()
	}

	if !changed && prev.BaseURL != "" && prev.BaseURL != p.BaseURL {
		if _, err := appendAd(schema.Advertisement{Entries: schema.NoEntries}); err != nil {
			return state.State{}, false, err
		}
	}

	if headCid.Defined() {
		if err := os.MkdirAll(p.AdDir, 0o755); err != nil {
			return state.State{}, false, fmt.Errorf("ad dir: %w", err)
		}
		sh, err := head.NewSignedHead(headCid, Topic, p.Key)
		if err != nil {
			return state.State{}, false, fmt.Errorf("signed head: %w", err)
		}
		data, err := sh.Encode()
		if err != nil {
			return state.State{}, false, fmt.Errorf("encode signed head: %w", err)
		}
		if err := writeFile(filepath.Join(p.AdDir, "head"), data); err != nil {
			return state.State{}, false, fmt.Errorf("write signed head: %w", err)
		}
	}

	st := state.State{
		Version: state.Version,
		PeerID:  id.String(),
		BaseURL: p.BaseURL,
		Roots:   make([]state.Root, 0, len(roots)),
	}
	if headCid.Defined() {
		st.Head = headCid.String()
	}
	for _, r := range roots {
		cidStr := r.CID.String()
		ad := addedAd[cidStr]
		if ad == "" {
			ad = prevAd[cidStr]
		}
		st.Roots = append(st.Roots, state.Root{Name: r.Name, CID: cidStr, Ad: ad})
	}
	return st, changed, nil
}

// writeFile writes data to path atomically via a temp file in the same directory.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
