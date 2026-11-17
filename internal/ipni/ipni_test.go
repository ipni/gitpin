package ipni

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/gitpin/internal/dag"
	"github.com/ipni/gitpin/internal/state"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
)

const wantPeerID = "12D3KooWA4Xop1JaT3MHxwYMkCepYsv4iPVopMXwCz5iHYdBfeSB"

func testKey(t *testing.T) crypto.PrivKey {
	t.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	key, err := crypto.UnmarshalEd25519PrivateKey(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatalf("unmarshal key: %v", err)
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatalf("peer id: %v", err)
	}
	if id.String() != wantPeerID {
		t.Fatalf("peer id = %s, want %s", id.String(), wantPeerID)
	}
	return key
}

func testParams(t *testing.T) Params {
	t.Helper()
	return Params{
		Key:     testKey(t),
		Addr:    multiaddr.StringCast("/dns/alice.github.io/tcp/443/https"),
		BaseURL: "https://alice.github.io",
		AdDir:   t.TempDir(),
	}
}

func rawCid(data []byte) cid.Cid {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		panic(err)
	}
	return cid.NewCidV1(cid.Raw, mh)
}

func mkRoot(name, tag string, blocks int) dag.Root {
	root := dag.Root{Name: name, CID: rawCid([]byte("root-" + tag))}
	for i := 0; i < blocks; i++ {
		root.Blocks = append(root.Blocks, rawCid([]byte("block-"+tag+"-"+string(rune('a'+i)))))
	}
	return root
}

func readFile(t *testing.T, dir string, c cid.Cid) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, c.String()))
	if err != nil {
		t.Fatalf("read %s: %v", c, err)
	}
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if !bytes.Equal(mh, c.Hash()) {
		t.Fatalf("%s: multihash of stored bytes does not match cid", c)
	}
	return data
}

func loadAd(t *testing.T, dir string, c cid.Cid) schema.Advertisement {
	t.Helper()
	ad, err := schema.BytesToAdvertisement(c, readFile(t, dir, c))
	if err != nil {
		t.Fatalf("decode ad %s: %v", c, err)
	}
	return ad
}

func loadChunk(t *testing.T, dir string, c cid.Cid) schema.EntryChunk {
	t.Helper()
	chunk, err := schema.BytesToEntryChunk(c, readFile(t, dir, c))
	if err != nil {
		t.Fatalf("decode entry chunk %s: %v", c, err)
	}
	return chunk
}

func loadHead(t *testing.T, dir string) *head.SignedHead {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "head"))
	if err != nil {
		t.Fatalf("read head: %v", err)
	}
	sh, err := head.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode head: %v", err)
	}
	return sh
}

func cidOfLink(lnk ipld.Link) cid.Cid {
	return lnk.(cidlink.Link).Cid
}

func fileList(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestFirstPublish(t *testing.T) {
	p := testParams(t)
	root := mkRoot("a.txt", "a", 3)

	st, changed, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{root})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if st.Version != 1 {
		t.Errorf("version = %d, want 1", st.Version)
	}
	if st.PeerID != wantPeerID {
		t.Errorf("peer id = %s, want %s", st.PeerID, wantPeerID)
	}
	if st.BaseURL != "https://alice.github.io" {
		t.Errorf("base url = %q", st.BaseURL)
	}
	if st.Head == "" {
		t.Fatal("head is empty")
	}
	if len(st.Roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(st.Roots))
	}
	if st.Roots[0].Name != "a.txt" || st.Roots[0].CID != root.CID.String() || st.Roots[0].Ad != st.Head {
		t.Errorf("roots = %+v", st.Roots[0])
	}

	adCid, err := cid.Parse(st.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	ad := loadAd(t, p.AdDir, adCid)
	signer, err := ad.VerifySignature()
	if err != nil {
		t.Fatalf("verify signature: %v", err)
	}
	if signer.String() != wantPeerID {
		t.Errorf("signer = %s, want %s", signer, wantPeerID)
	}
	if ad.PreviousID != nil {
		t.Errorf("previous id = %v, want nil", ad.PreviousID)
	}
	if ad.Provider != wantPeerID {
		t.Errorf("provider = %s, want %s", ad.Provider, wantPeerID)
	}
	if len(ad.Addresses) != 1 || ad.Addresses[0] != "/dns/alice.github.io/tcp/443/https" {
		t.Errorf("addresses = %v", ad.Addresses)
	}
	if !bytes.Equal(ad.ContextID, root.CID.Bytes()) {
		t.Errorf("context id = %x, want %x", ad.ContextID, root.CID.Bytes())
	}
	if !bytes.Equal(ad.Metadata, []byte{0xa0, 0x12, 0x00}) {
		t.Errorf("metadata = %x, want a01200", ad.Metadata)
	}
	if ad.IsRm {
		t.Error("IsRm = true, want false")
	}

	chunk := loadChunk(t, p.AdDir, cidOfLink(ad.Entries))
	if len(chunk.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(chunk.Entries))
	}
	for i, b := range root.Blocks {
		if !bytes.Equal(chunk.Entries[i], b.Hash()) {
			t.Errorf("entry %d = %x, want %x", i, chunk.Entries[i], b.Hash())
		}
	}
	if chunk.Next != nil {
		t.Errorf("chunk next = %v, want nil", chunk.Next)
	}

	sh := loadHead(t, p.AdDir)
	if _, err := sh.Validate(); err != nil {
		t.Fatalf("validate signed head: %v", err)
	}
	if cidOfLink(sh.Head) != adCid {
		t.Errorf("signed head cid = %s, want %s", cidOfLink(sh.Head), adCid)
	}
	if sh.Topic == nil || *sh.Topic != Topic {
		t.Errorf("signed head topic = %v, want %s", sh.Topic, Topic)
	}
	if Topic != "/indexer/ingest/mainnet" {
		t.Errorf("topic = %s", Topic)
	}
}

func TestSecondPublishNoChange(t *testing.T) {
	p := testParams(t)
	roots := []dag.Root{mkRoot("a.txt", "a", 3), mkRoot("b.txt", "b", 2)}

	first, changed, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Fatal("changed = false on first publish")
	}
	before := fileList(p.AdDir)

	second, changed, err := Publish(p, first, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if changed {
		t.Error("changed = true, want false")
	}
	if second.Head != first.Head {
		t.Errorf("head = %s, want %s", second.Head, first.Head)
	}
	if after := fileList(p.AdDir); !equalStrings(before, after) {
		t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
	}
	if _, err := os.Stat(filepath.Join(p.AdDir, "head")); err != nil {
		t.Errorf("head file: %v", err)
	}
	if sh := loadHead(t, p.AdDir); cidOfLink(sh.Head) != mustCid(t, second.Head) {
		t.Errorf("signed head cid = %s, want %s", cidOfLink(sh.Head), second.Head)
	}
	if second.Roots[0].Ad != first.Roots[0].Ad || second.Roots[1].Ad != first.Roots[1].Ad {
		t.Errorf("ads not carried over: %+v", second.Roots)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDeterminism(t *testing.T) {
	roots := []dag.Root{mkRoot("a.txt", "a", 3), mkRoot("b.txt", "b", 2)}

	p1 := testParams(t)
	p2 := testParams(t)
	s1, _, err := Publish(p1, state.State{Version: 1, Roots: []state.Root{}}, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	s2, _, err := Publish(p2, state.State{Version: 1, Roots: []state.Root{}}, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if s1.Head != s2.Head {
		t.Errorf("heads differ: %s vs %s", s1.Head, s2.Head)
	}
	if s1.Roots[0].Ad != s2.Roots[0].Ad {
		t.Errorf("ad cids differ: %s vs %s", s1.Roots[0].Ad, s2.Roots[0].Ad)
	}
}

func TestRemoveAndAdd(t *testing.T) {
	p := testParams(t)
	a := mkRoot("a.txt", "a", 3)
	b := mkRoot("b.txt", "b", 2)
	c := mkRoot("c.txt", "c", 1)

	prev, _, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{a, b})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	oldHead, err := cid.Parse(prev.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	aAdCid, err := cid.Parse(prev.Roots[0].Ad)
	if err != nil {
		t.Fatalf("parse ad: %v", err)
	}
	aChunkCid := cidOfLink(loadAd(t, p.AdDir, aAdCid).Entries)
	before := fileList(p.AdDir)

	st, changed, err := Publish(p, prev, []dag.Root{b, c})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}
	if len(st.Roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(st.Roots))
	}
	if st.Roots[0].Name != "b.txt" || st.Roots[0].Ad != prev.Roots[1].Ad {
		t.Errorf("b root = %+v, want ad %s", st.Roots[0], prev.Roots[1].Ad)
	}
	if st.Roots[1].Name != "c.txt" || st.Roots[1].Ad == "" || st.Roots[1].Ad == prev.Roots[1].Ad {
		t.Errorf("c root = %+v", st.Roots[1])
	}
	if st.Head != st.Roots[1].Ad {
		t.Errorf("head = %s, want c ad %s", st.Head, st.Roots[1].Ad)
	}
	if sh := loadHead(t, p.AdDir); cidOfLink(sh.Head) != mustCid(t, st.Head) {
		t.Errorf("signed head cid = %s, want %s", cidOfLink(sh.Head), st.Head)
	}

	addCid, err := cid.Parse(st.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	addAd := loadAd(t, p.AdDir, addCid)
	if addAd.IsRm {
		t.Error("add ad IsRm = true")
	}
	if !bytes.Equal(addAd.ContextID, c.CID.Bytes()) {
		t.Errorf("add context id = %x, want %x", addAd.ContextID, c.CID.Bytes())
	}
	rmCid := cidOfLink(addAd.PreviousID)
	if rmCid == oldHead {
		t.Fatal("add ad previous is the old head, want the rm ad")
	}
	rmAd := loadAd(t, p.AdDir, rmCid)
	if !rmAd.IsRm {
		t.Error("rm ad IsRm = false")
	}
	if !bytes.Equal(rmAd.ContextID, a.CID.Bytes()) {
		t.Errorf("rm context id = %x, want %x", rmAd.ContextID, a.CID.Bytes())
	}
	if rmAd.Entries != schema.NoEntries {
		t.Errorf("rm entries = %v, want NoEntries", rmAd.Entries)
	}
	if len(rmAd.Metadata) != 0 {
		t.Errorf("rm metadata = %x, want empty", rmAd.Metadata)
	}
	if cidOfLink(rmAd.PreviousID) != oldHead {
		t.Errorf("rm previous = %s, want old head %s", cidOfLink(rmAd.PreviousID), oldHead)
	}

	after := fileList(p.AdDir)
	if len(after) != len(before)+3 {
		t.Errorf("files added = %d, want 3:\nbefore %v\nafter  %v", len(after)-len(before), before, after)
	}
	for _, name := range []string{oldHead.String(), aChunkCid.String(), aAdCid.String()} {
		if !strings.Contains(strings.Join(after, ","), name) {
			t.Errorf("file %s was removed", name)
		}
	}
}

func TestRemoveEverything(t *testing.T) {
	p := testParams(t)
	a := mkRoot("a.txt", "a", 2)

	prev, _, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{a})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	oldHead, err := cid.Parse(prev.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}

	st, changed, err := Publish(p, prev, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}
	if st.Roots == nil {
		t.Fatal("roots is nil, want empty non-nil slice")
	}
	if len(st.Roots) != 0 {
		t.Errorf("roots = %+v, want empty", st.Roots)
	}
	adCid, err := cid.Parse(st.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	if adCid == oldHead {
		t.Fatal("head did not change")
	}
	ad := loadAd(t, p.AdDir, adCid)
	if !ad.IsRm {
		t.Error("IsRm = false, want true")
	}
	if !bytes.Equal(ad.ContextID, a.CID.Bytes()) {
		t.Errorf("context id = %x, want %x", ad.ContextID, a.CID.Bytes())
	}
	if ad.Entries != schema.NoEntries {
		t.Errorf("entries = %v, want NoEntries", ad.Entries)
	}
	if cidOfLink(ad.PreviousID) != oldHead {
		t.Errorf("previous = %s, want %s", cidOfLink(ad.PreviousID), oldHead)
	}
	if sh := loadHead(t, p.AdDir); cidOfLink(sh.Head) != adCid {
		t.Errorf("signed head cid = %s, want %s", cidOfLink(sh.Head), adCid)
	}
}

func TestSameCidTwoNames(t *testing.T) {
	p := testParams(t)
	shared := mkRoot("x.txt", "s", 2)
	shared.CID = rawCid([]byte("root-shared"))
	shared.Blocks = []cid.Cid{rawCid([]byte("block-shared-1")), rawCid([]byte("block-shared-2"))}
	y := shared
	y.Name = "y.txt"

	st, changed, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{shared, y})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}
	if st.Roots[0].Ad != st.Roots[1].Ad {
		t.Errorf("ads differ: %s vs %s", st.Roots[0].Ad, st.Roots[1].Ad)
	}
	if st.Roots[0].Ad != st.Head {
		t.Errorf("ad = %s, want head %s", st.Roots[0].Ad, st.Head)
	}
	before := fileList(p.AdDir)

	// Removing only one name of the shared CID changes nothing.
	st2, changed, err := Publish(p, st, []dag.Root{y})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if changed {
		t.Error("changed = true after removing one name of a shared cid")
	}
	if after := fileList(p.AdDir); !equalStrings(before, after) {
		t.Errorf("files changed: %v vs %v", before, after)
	}
	if len(st2.Roots) != 1 || st2.Roots[0].Ad != st.Roots[0].Ad {
		t.Errorf("roots = %+v", st2.Roots)
	}

	// Renaming keeps the same ad.
	z := shared
	z.Name = "z.txt"
	st3, changed, err := Publish(p, st, []dag.Root{z, y})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if changed {
		t.Error("changed = true after rename")
	}
	if st3.Roots[0].Name != "z.txt" || st3.Roots[0].Ad != st.Roots[0].Ad {
		t.Errorf("roots = %+v", st3.Roots)
	}
}

func TestReAddMakesNewAd(t *testing.T) {
	p := testParams(t)
	a := mkRoot("a.txt", "a", 2)
	b := mkRoot("b.txt", "b", 2)

	first, _, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{a, b})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	firstAd := first.Roots[0].Ad

	removed, _, err := Publish(p, first, []dag.Root{b})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	readded, changed, err := Publish(p, removed, []dag.Root{a, b})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}
	if readded.Roots[0].CID != a.CID.String() {
		t.Errorf("roots = %+v", readded.Roots)
	}
	if readded.Roots[0].Ad == firstAd {
		t.Fatalf("re-add reused the first ad cid %s", firstAd)
	}
	adCid, err := cid.Parse(readded.Roots[0].Ad)
	if err != nil {
		t.Fatalf("parse ad: %v", err)
	}
	ad := loadAd(t, p.AdDir, adCid)
	if ad.IsRm {
		t.Error("re-add ad IsRm = true")
	}
	if !bytes.Equal(ad.ContextID, a.CID.Bytes()) {
		t.Errorf("context id = %x, want %x", ad.ContextID, a.CID.Bytes())
	}
	if cidOfLink(ad.PreviousID) != mustCid(t, removed.Head) {
		t.Errorf("re-add previous = %s, want remove ad %s", cidOfLink(ad.PreviousID), removed.Head)
	}
	if adCid.String() == firstAd {
		t.Errorf("re-add ad cid = %s, want it to differ from the first add ad %s", adCid, firstAd)
	}
}

func TestChunking(t *testing.T) {
	entriesPerChunk = 2
	t.Cleanup(func() { entriesPerChunk = EntriesPerChunk })

	p := testParams(t)
	root := mkRoot("a.txt", "a", 5)

	st, changed, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{root})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Fatal("changed = false")
	}
	adCid, err := cid.Parse(st.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	ad := loadAd(t, p.AdDir, adCid)

	var all []multihash.Multihash
	lnk := ad.Entries
	sizes := []int{}
	for i := 0; lnk != nil; i++ {
		if i > 5 {
			t.Fatal("entry chunk chain too long")
		}
		chunk := loadChunk(t, p.AdDir, cidOfLink(lnk))
		sizes = append(sizes, len(chunk.Entries))
		all = append(all, chunk.Entries...)
		lnk = chunk.Next
	}
	if !equalInts(sizes, []int{2, 2, 1}) {
		t.Errorf("chunk sizes = %v, want [2 2 1]", sizes)
	}
	if len(all) != 5 {
		t.Fatalf("entries = %d, want 5", len(all))
	}
	for i, b := range root.Blocks {
		if !bytes.Equal(all[i], b.Hash()) {
			t.Errorf("entry %d = %x, want %x", i, all[i], b.Hash())
		}
	}
}

func TestChunkingBoundary(t *testing.T) {
	entriesPerChunk = 2
	t.Cleanup(func() { entriesPerChunk = EntriesPerChunk })

	p := testParams(t)
	root := mkRoot("a.txt", "a", 4)

	st, _, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{root})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	adCid, err := cid.Parse(st.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	ad := loadAd(t, p.AdDir, adCid)

	sizes := []int{}
	for lnk := ad.Entries; lnk != nil; {
		chunk := loadChunk(t, p.AdDir, cidOfLink(lnk))
		sizes = append(sizes, len(chunk.Entries))
		lnk = chunk.Next
	}
	if !equalInts(sizes, []int{2, 2}) {
		t.Errorf("chunk sizes = %v, want [2 2]", sizes)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBaseURLChange(t *testing.T) {
	p := testParams(t)
	roots := []dag.Root{mkRoot("a.txt", "a", 2)}

	prev, _, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	before := fileList(p.AdDir)

	p.Addr = multiaddr.StringCast("/dns/bob.example.com/tcp/443/https")
	p.BaseURL = "https://bob.example.com"
	st, changed, err := Publish(p, prev, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if st.BaseURL != "https://bob.example.com" {
		t.Errorf("base url = %q", st.BaseURL)
	}
	after := fileList(p.AdDir)
	if len(after) != len(before)+1 {
		t.Errorf("files added = %d, want 1:\nbefore %v\nafter  %v", len(after)-len(before), before, after)
	}
	adCid, err := cid.Parse(st.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	ad := loadAd(t, p.AdDir, adCid)
	if len(ad.Addresses) != 1 || ad.Addresses[0] != "/dns/bob.example.com/tcp/443/https" {
		t.Errorf("addresses = %v", ad.Addresses)
	}
	if len(ad.ContextID) != 0 {
		t.Errorf("context id = %x, want empty", ad.ContextID)
	}
	if ad.Entries != schema.NoEntries {
		t.Errorf("entries = %v, want NoEntries", ad.Entries)
	}
	if ad.IsRm {
		t.Error("IsRm = true")
	}
	if cidOfLink(ad.PreviousID) != mustCid(t, prev.Head) {
		t.Errorf("previous = %s, want %s", cidOfLink(ad.PreviousID), prev.Head)
	}
	if sh := loadHead(t, p.AdDir); cidOfLink(sh.Head) != adCid {
		t.Errorf("signed head cid = %s, want %s", cidOfLink(sh.Head), adCid)
	}
	for _, r := range st.Roots {
		if r.Ad != prev.Roots[0].Ad {
			t.Errorf("root ad = %s, want %s", r.Ad, prev.Roots[0].Ad)
		}
	}
}

func TestBaseURLChangeWithAdd(t *testing.T) {
	p := testParams(t)
	a := mkRoot("a.txt", "a", 2)

	prev, _, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, []dag.Root{a})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	before := fileList(p.AdDir)

	p.Addr = multiaddr.StringCast("/dns/bob.example.com/tcp/443/https")
	p.BaseURL = "https://bob.example.com"
	b := mkRoot("b.txt", "b", 1)
	st, changed, err := Publish(p, prev, []dag.Root{a, b})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !changed {
		t.Fatal("changed = false")
	}
	after := fileList(p.AdDir)
	if len(after) != len(before)+2 {
		t.Errorf("files added = %d, want 2:\nbefore %v\nafter  %v", len(after)-len(before), before, after)
	}
	adCid, err := cid.Parse(st.Head)
	if err != nil {
		t.Fatalf("parse head: %v", err)
	}
	ad := loadAd(t, p.AdDir, adCid)
	if !bytes.Equal(ad.ContextID, b.CID.Bytes()) {
		t.Errorf("head ad is not the add ad: context id = %x", ad.ContextID)
	}
	if len(ad.Addresses) != 1 || ad.Addresses[0] != "/dns/bob.example.com/tcp/443/https" {
		t.Errorf("addresses = %v", ad.Addresses)
	}
}

func TestBaseURLChangeWithEmptyPrevBaseURL(t *testing.T) {
	p := testParams(t)
	roots := []dag.Root{mkRoot("a.txt", "a", 2)}

	prev, _, err := Publish(p, state.State{Version: 1, Roots: []state.Root{}}, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	prev.BaseURL = ""

	st, changed, err := Publish(p, prev, roots)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if changed {
		t.Error("changed = true, want false")
	}
	if st.BaseURL != p.BaseURL {
		t.Errorf("base url = %q, want %q", st.BaseURL, p.BaseURL)
	}
}

func mustCid(t *testing.T, s string) cid.Cid {
	t.Helper()
	c, err := cid.Parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return c
}

func TestWrongKey(t *testing.T) {
	p := testParams(t)
	prev := state.State{Version: 1, PeerID: "12D3KooWKnRvJpBcdA1eP6fHZGvNPxLJH9cM6nBqThpT7rR8Vd7d", Roots: []state.Root{}}

	_, changed, err := Publish(p, prev, []dag.Root{mkRoot("a.txt", "a", 1)})
	if err == nil {
		t.Fatal("err = nil, want key mismatch error")
	}
	if changed {
		t.Error("changed = true")
	}
	if !strings.Contains(err.Error(), "GITPIN_KEY belongs to") {
		t.Errorf("err = %v, want it to mention GITPIN_KEY belongs to", err)
	}
	if !strings.Contains(err.Error(), "delete gitpin.json") {
		t.Errorf("err = %v, want it to mention delete gitpin.json", err)
	}
}

func TestBadPrevHead(t *testing.T) {
	p := testParams(t)
	prev := state.State{Version: 1, PeerID: wantPeerID, Head: "nope", Roots: []state.Root{}}

	_, _, err := Publish(p, prev, []dag.Root{mkRoot("a.txt", "a", 1)})
	if err == nil {
		t.Fatal("err = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v, want it to mention the bad head", err)
	}
}
