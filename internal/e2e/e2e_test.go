package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-unixfsnode"
	"github.com/ipfs/go-unixfsnode/file"
	dagpb "github.com/ipld/go-codec-dagpb"
	"github.com/ipld/go-ipld-prime"
	_ "github.com/ipld/go-ipld-prime/codec/raw"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	basicnode "github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/storage/memstore"
	selectorparse "github.com/ipld/go-ipld-prime/traversal/selector/parse"
	"github.com/ipni/gitpin/internal/cli"
	"github.com/ipni/gitpin/internal/site"
	"github.com/ipni/go-libipni/announce/message"
	"github.com/ipni/go-libipni/dagsync/ipnisync"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
)

const (
	vectorKey      = "CAESQAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fA6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg="
	vectorPeerID   = "12D3KooWA4Xop1JaT3MHxwYMkCepYsv4iPVopMXwCz5iHYdBfeSB"
	otherPeerID    = "12D3KooWKnRvJpBcdA1eP6fHZGvNPxLJH9cM6nBqThpT7rR8Vd7d"
	probeCID       = "bafkqaaa"
	metadataGoIPFS = "\xa0\x12\x00"
)

// publishSummary mirrors the JSON line printed by the publish subcommand.
type publishSummary struct {
	Changed bool   `json:"changed"`
	Head    string `json:"head"`
	PeerID  string `json:"peer_id"`
	Roots   []struct {
		Name string `json:"name"`
		CID  string `json:"cid"`
	} `json:"roots"`
}

// adInfo pairs an advertisement with the CID it is stored under.
type adInfo struct {
	Cid cid.Cid
	Ad  schema.Advertisement
}

func mustPeer(t *testing.T) peer.ID {
	t.Helper()
	id, err := peer.Decode(vectorPeerID)
	if err != nil {
		t.Fatalf("decode vector peer: %v", err)
	}
	return id
}

func mustBaseAddr(t *testing.T, baseURL string) multiaddr.Multiaddr {
	t.Helper()
	a, err := site.BaseAddr(baseURL)
	if err != nil {
		t.Fatalf("base addr for %s: %v", baseURL, err)
	}
	return a
}

// runCLI invokes cli.Run and returns its exit code plus captured stdout/stderr.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func decodeSummary(t *testing.T, stdout string) publishSummary {
	t.Helper()
	var s publishSummary
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("decode publish summary %q: %v", stdout, err)
	}
	return s
}

// httpLinkSystem builds a LinkSystem that fetches blocks over the trustless
// gateway path of a static server and verifies each block's multihash.
func httpLinkSystem(baseURL string) ipld.LinkSystem {
	ls := cidlink.DefaultLinkSystem()
	ls.StorageReadOpener = func(lc ipld.LinkContext, l ipld.Link) (io.Reader, error) {
		cl, ok := l.(cidlink.Link)
		if !ok {
			return nil, fmt.Errorf("unexpected link type %T", l)
		}
		ctx := lc.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		u := strings.TrimRight(baseURL, "/") + "/ipfs/" + cl.Cid.String() + "?format=raw"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.ipld.raw")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		sum, err := multihash.Sum(body, cl.Cid.Prefix().MhType, cl.Cid.Prefix().MhLength)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(sum, cl.Cid.Hash()) {
			return nil, fmt.Errorf("hash mismatch for %s", cl.Cid)
		}
		return bytes.NewReader(body), nil
	}
	return ls
}

func protoForCodec(c cid.Cid) ipld.NodePrototype {
	if c.Prefix().Codec == uint64(cid.DagProtobuf) {
		return dagpb.Type.PBNode
	}
	return basicnode.Prototype.Bytes
}

// readFileBytes loads a UnixFS file root over the gateway and returns its bytes.
func readFileBytes(ctx context.Context, ls *ipld.LinkSystem, c cid.Cid) ([]byte, error) {
	node, err := ls.Load(ipld.LinkContext{Ctx: ctx}, cidlink.Link{Cid: c}, protoForCodec(c))
	if err != nil {
		return nil, err
	}
	f, err := file.NewUnixFSFile(ctx, node, ls)
	if err != nil {
		return nil, err
	}
	r, err := f.AsLargeBytes()
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// dirChildLink resolves a named child link of a UnixFS directory root.
func dirChildLink(ctx context.Context, ls *ipld.LinkSystem, dirCid cid.Cid, name string) (cid.Cid, error) {
	node, err := ls.Load(ipld.LinkContext{Ctx: ctx}, cidlink.Link{Cid: dirCid}, dagpb.Type.PBNode)
	if err != nil {
		return cid.Undef, err
	}
	dir, err := unixfsnode.Reify(ipld.LinkContext{Ctx: ctx}, node, ls)
	if err != nil {
		return cid.Undef, err
	}
	lnkNode, err := dir.LookupByString(name)
	if err != nil {
		return cid.Undef, err
	}
	lnk, err := lnkNode.AsLink()
	if err != nil {
		return cid.Undef, err
	}
	cl, ok := lnk.(cidlink.Link)
	if !ok {
		return cid.Undef, fmt.Errorf("unexpected link type %T", lnk)
	}
	return cl.Cid, nil
}

// newSyncer wires the real go-libipni HTTP sync client against a static server.
func newSyncer(t *testing.T, baseURL string, id peer.ID) (*ipnisync.Syncer, *memstore.Store) {
	t.Helper()
	ls := cidlink.DefaultLinkSystem()
	store := &memstore.Store{}
	ls.SetReadStorage(store)
	ls.SetWriteStorage(store)
	sync := ipnisync.NewSync(ls, nil)
	t.Cleanup(sync.Close)
	syncer, err := sync.NewSyncer(peer.AddrInfo{ID: id, Addrs: []multiaddr.Multiaddr{mustBaseAddr(t, baseURL)}})
	if err != nil {
		t.Fatalf("new syncer: %v", err)
	}
	return syncer, store
}

// walkAds walks the advertisement chain from head (head first), syncing each ad
// block and, for add ads, its entry-chunk chain. It never walks an ad with
// ExploreAllRecursively, which would try to fetch schema.NoEntries. Every ad it
// returns, add or removal, must be signed by wantPeer and must list wantAddr as
// its only address.
func walkAds(ctx context.Context, syncer *ipnisync.Syncer, store *memstore.Store, head cid.Cid, wantPeer peer.ID, wantAddr string) ([]adInfo, error) {
	var ads []adInfo
	c := head
	for c.Defined() {
		if err := syncer.Sync(ctx, c, selectorparse.CommonSelector_MatchPoint); err != nil {
			return nil, fmt.Errorf("sync ad %s: %w", c, err)
		}
		data, err := store.Get(ctx, string(c.Bytes()))
		if err != nil {
			return nil, fmt.Errorf("read ad %s: %w", c, err)
		}
		ad, err := schema.BytesToAdvertisement(c, data)
		if err != nil {
			return nil, fmt.Errorf("decode ad %s: %w", c, err)
		}
		signer, err := ad.VerifySignature()
		if err != nil {
			return nil, fmt.Errorf("verify ad %s: %w", c, err)
		}
		if signer != wantPeer {
			return nil, fmt.Errorf("ad %s signed by %s, want %s", c, signer, wantPeer)
		}
		if len(ad.Addresses) != 1 || ad.Addresses[0] != wantAddr {
			return nil, fmt.Errorf("ad %s addresses = %v, want [%s]", c, ad.Addresses, wantAddr)
		}
		ads = append(ads, adInfo{Cid: c, Ad: ad})
		if ad.Entries != nil && ad.Entries != schema.NoEntries {
			ec, ok := ad.Entries.(cidlink.Link)
			if !ok {
				return nil, fmt.Errorf("ad %s entries link type %T", c, ad.Entries)
			}
			if err := syncer.Sync(ctx, ec.Cid, selectorparse.CommonSelector_ExploreAllRecursively); err != nil {
				return nil, fmt.Errorf("sync entries %s: %w", ec.Cid, err)
			}
		}
		if ad.PreviousID == nil {
			break
		}
		prev, ok := ad.PreviousID.(cidlink.Link)
		if !ok {
			return nil, fmt.Errorf("ad %s previous link type %T", c, ad.PreviousID)
		}
		c = prev.Cid
	}
	return ads, nil
}

// entryMultihashes reads an entry-chunk chain from the store and returns the set
// of multihashes it lists, keyed by the multihash bytes.
func entryMultihashes(ctx context.Context, store *memstore.Store, head cid.Cid) (map[string]bool, error) {
	set := map[string]bool{}
	c := head
	for c.Defined() {
		data, err := store.Get(ctx, string(c.Bytes()))
		if err != nil {
			return nil, fmt.Errorf("read entry chunk %s: %w", c, err)
		}
		ec, err := schema.BytesToEntryChunk(c, data)
		if err != nil {
			return nil, fmt.Errorf("decode entry chunk %s: %w", c, err)
		}
		for _, mh := range ec.Entries {
			set[string(mh)] = true
		}
		if ec.Next == nil {
			break
		}
		n, ok := ec.Next.(cidlink.Link)
		if !ok {
			return nil, fmt.Errorf("entry chunk %s next link type %T", c, ec.Next)
		}
		c = n.Cid
	}
	return set, nil
}

// ipfsBlockMultihashes returns the multihash set of every block file in ipfsDir
// except the probe CID. Every file's bytes are re-hashed and must match the CID
// parsed from its name; any non-CID file name (e.g. a leftover .tmp) is an error.
func ipfsBlockMultihashes(ipfsDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(ipfsDir)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("ipfs dir has non-regular entry %s", e.Name())
		}
		if e.Name() == probeCID {
			continue
		}
		if strings.HasSuffix(e.Name(), ".tmp") {
			return nil, fmt.Errorf("ipfs dir has temp file %s", e.Name())
		}
		c, err := cid.Decode(e.Name())
		if err != nil {
			return nil, fmt.Errorf("ipfs dir has non-CID file %s: %w", e.Name(), err)
		}
		data, err := os.ReadFile(filepath.Join(ipfsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		sum, err := multihash.Sum(data, c.Prefix().MhType, c.Prefix().MhLength)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(sum, c.Hash()) {
			return nil, fmt.Errorf("ipfs file %s hashes to %s, want %s", e.Name(), sum.B58String(), c.Hash().B58String())
		}
		set[string(c.Hash())] = true
	}
	return set, nil
}

func mustListAdFiles(t *testing.T, siteDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(siteDir, "ipni", "v1", "ad"))
	if err != nil {
		t.Fatalf("read ad dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func sameStringSlice(a, b []string) bool {
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

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func portOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url %s: %v", rawURL, err)
	}
	return u.Port()
}

func TestEndToEnd(t *testing.T) {
	t.Setenv("GITPIN_KEY", vectorKey)
	id := mustPeer(t)
	ctx := context.Background()

	// Shared state across the ordered subtests. The directories and static
	// servers live for the whole test so the subtests can build on each other.
	var (
		contentDir = t.TempDir()
		siteDir    = t.TempDir()
		srv        = httptest.NewServer(http.FileServer(http.Dir(siteDir)))
		baseURL    = srv.URL

		s2       = t.TempDir()
		srv2     = httptest.NewServer(http.FileServer(http.Dir(s2)))
		baseURL2 = srv2.URL

		step1        publishSummary
		step1Roots   = map[string]cid.Cid{}
		step1AdFiles []string
		updateRoots  = map[string]cid.Cid{}
		adFileCount  int
		unpinHead    string

		s2Head     string
		s2Roots    = map[string]cid.Cid{}
		announceMu sync.Mutex
		announced  [][]byte
	)
	t.Cleanup(srv.Close)
	t.Cleanup(srv2.Close)

	helloContent := []byte("Hello from my first IPFS pin!\n")
	smallContent := []byte("a small text file under photos\n")
	rng := rand.New(rand.NewSource(0x2a16b0))
	bigContent := make([]byte, 700*1024)
	if _, err := rng.Read(bigContent); err != nil {
		t.Fatalf("gen big.bin: %v", err)
	}

	t.Run("first publish", func(t *testing.T) {
		writeFile(t, filepath.Join(contentDir, "hello.txt"), helloContent)
		writeFile(t, filepath.Join(contentDir, "photos", "big.bin"), bigContent)
		writeFile(t, filepath.Join(contentDir, "photos", "small.txt"), smallContent)

		code, stdout, stderr := runCLI(t, "publish", "--content", contentDir, "--site", siteDir, "--base-url", baseURL)
		if code != 0 {
			t.Fatalf("publish exit %d, stderr: %s", code, stderr)
		}
		step1 = decodeSummary(t, stdout)
		if !step1.Changed {
			t.Errorf("changed = false, want true")
		}
		if step1.PeerID != vectorPeerID {
			t.Errorf("peer_id = %q, want %q", step1.PeerID, vectorPeerID)
		}
		if len(step1.Roots) != 2 {
			t.Fatalf("roots = %d, want 2 (%+v)", len(step1.Roots), step1.Roots)
		}
		names := map[string]bool{}
		for _, r := range step1.Roots {
			names[r.Name] = true
			c, err := cid.Decode(r.CID)
			if err != nil {
				t.Fatalf("root %s cid %q: %v", r.Name, r.CID, err)
			}
			step1Roots[r.Name] = c
		}
		if !names["hello.txt"] || !names["photos"] {
			t.Errorf("root names = %v, want hello.txt and photos", names)
		}

		for _, f := range []string{".nojekyll", "index.html", "gitpin.json"} {
			if _, err := os.Stat(filepath.Join(siteDir, f)); err != nil {
				t.Errorf("site missing %s: %v", f, err)
			}
		}
		idx, err := os.ReadFile(filepath.Join(siteDir, "index.html"))
		if err == nil {
			for _, r := range step1.Roots {
				if !bytes.Contains(idx, []byte(r.CID)) {
					t.Errorf("index.html missing CID %s", r.CID)
				}
			}
		}
		probe := filepath.Join(siteDir, "ipfs", probeCID)
		if fi, err := os.Stat(probe); err != nil {
			t.Errorf("missing ipfs/%s: %v", probeCID, err)
		} else if fi.Size() != 0 {
			t.Errorf("ipfs/%s size = %d, want 0", probeCID, fi.Size())
		}

		step1AdFiles = mustListAdFiles(t, siteDir)
		adFileCount = len(step1AdFiles)
	})

	t.Run("indexer view", func(t *testing.T) {
		syncer, store := newSyncer(t, baseURL, id)
		head, err := syncer.GetHead(ctx)
		if err != nil {
			t.Fatalf("get head: %v", err)
		}
		if head.String() != step1.Head {
			t.Fatalf("GetHead = %s, want state head %s", head, step1.Head)
		}
		addrStr := mustBaseAddr(t, baseURL).String()
		ads, err := walkAds(ctx, syncer, store, head, id, addrStr)
		if err != nil {
			t.Fatalf("walk ads: %v", err)
		}
		if len(ads) != 2 {
			t.Fatalf("ads = %d, want 2", len(ads))
		}
		wantCtx := map[string]bool{
			string(step1Roots["hello.txt"].Bytes()): true,
			string(step1Roots["photos"].Bytes()):    true,
		}
		gotCtx := map[string]bool{}
		union := map[string]bool{}
		for _, a := range ads {
			if a.Ad.IsRm {
				t.Errorf("ad %s is a removal, want add", a.Cid)
			}
			if a.Ad.Entries == nil || a.Ad.Entries == schema.NoEntries {
				t.Errorf("ad %s has no entries, want add ad", a.Cid)
				continue
			}
			// Signature and addresses are checked for every ad by walkAds.
			if !bytes.Equal(a.Ad.Metadata, []byte(metadataGoIPFS)) {
				t.Errorf("ad %s metadata = %x, want %x", a.Cid, a.Ad.Metadata, []byte(metadataGoIPFS))
			}
			gotCtx[string(a.Ad.ContextID)] = true
			ec, ok := a.Ad.Entries.(cidlink.Link)
			if !ok {
				t.Fatalf("ad %s entries link type %T", a.Cid, a.Ad.Entries)
			}
			mh, err := entryMultihashes(ctx, store, ec.Cid)
			if err != nil {
				t.Fatalf("entry multihashes: %v", err)
			}
			for k := range mh {
				union[k] = true
			}
		}
		if !sameSet(gotCtx, wantCtx) {
			t.Errorf("context IDs mismatch: got %d, want %d", len(gotCtx), len(wantCtx))
		}
		ipfsMH, err := ipfsBlockMultihashes(filepath.Join(siteDir, "ipfs"))
		if err != nil {
			t.Fatalf("ipfs block multihashes: %v", err)
		}
		if !sameSet(union, ipfsMH) {
			t.Errorf("entry multihash union (%d) != ipfs block multihashes (%d)", len(union), len(ipfsMH))
		}
	})

	t.Run("client view", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/ipfs/" + probeCID + "?format=raw")
		if err != nil {
			t.Fatalf("probe fetch: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("probe status = %d, want 200", resp.StatusCode)
		}

		ls := httpLinkSystem(baseURL)
		got, err := readFileBytes(ctx, &ls, step1Roots["hello.txt"])
		if err != nil {
			t.Fatalf("read hello.txt: %v", err)
		}
		if !bytes.Equal(got, helloContent) {
			t.Errorf("hello.txt bytes differ")
		}

		photosCid := step1Roots["photos"]
		bigCid, err := dirChildLink(ctx, &ls, photosCid, "big.bin")
		if err != nil {
			t.Fatalf("photos/big.bin link: %v", err)
		}
		gotBig, err := readFileBytes(ctx, &ls, bigCid)
		if err != nil {
			t.Fatalf("read photos/big.bin: %v", err)
		}
		if !bytes.Equal(gotBig, bigContent) {
			t.Errorf("photos/big.bin bytes differ")
		}
		smallCid, err := dirChildLink(ctx, &ls, photosCid, "small.txt")
		if err != nil {
			t.Fatalf("photos/small.txt link: %v", err)
		}
		gotSmall, err := readFileBytes(ctx, &ls, smallCid)
		if err != nil {
			t.Fatalf("read photos/small.txt: %v", err)
		}
		if !bytes.Equal(gotSmall, smallContent) {
			t.Errorf("photos/small.txt bytes differ")
		}
	})

	t.Run("idempotent re-run", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "publish", "--content", contentDir, "--site", siteDir, "--base-url", baseURL)
		if code != 0 {
			t.Fatalf("publish exit %d, stderr: %s", code, stderr)
		}
		s := decodeSummary(t, stdout)
		if s.Changed {
			t.Errorf("changed = true, want false")
		}
		if s.Head != step1.Head {
			t.Errorf("head = %s, want unchanged %s", s.Head, step1.Head)
		}
		now := mustListAdFiles(t, siteDir)
		if !sameStringSlice(now, step1AdFiles) {
			t.Errorf("ad file list changed on re-run:\n got %v\nwant %v", now, step1AdFiles)
		}
	})

	t.Run("update", func(t *testing.T) {
		if err := os.Remove(filepath.Join(contentDir, "hello.txt")); err != nil {
			t.Fatalf("remove hello.txt: %v", err)
		}
		writeFile(t, filepath.Join(contentDir, "notes.txt"), []byte("brand new notes with distinct bytes\n"))

		code, stdout, stderr := runCLI(t, "publish", "--content", contentDir, "--site", siteDir, "--base-url", baseURL)
		if code != 0 {
			t.Fatalf("publish exit %d, stderr: %s", code, stderr)
		}
		s := decodeSummary(t, stdout)
		if !s.Changed {
			t.Errorf("changed = false, want true")
		}
		for _, r := range s.Roots {
			c, err := cid.Decode(r.CID)
			if err != nil {
				t.Fatalf("update root %s cid %q: %v", r.Name, r.CID, err)
			}
			updateRoots[r.Name] = c
		}

		syncer, store := newSyncer(t, baseURL, id)
		head, err := syncer.GetHead(ctx)
		if err != nil {
			t.Fatalf("get head: %v", err)
		}
		if head.String() != s.Head {
			t.Fatalf("GetHead = %s, want %s", head, s.Head)
		}
		ads, err := walkAds(ctx, syncer, store, head, id, mustBaseAddr(t, baseURL).String())
		if err != nil {
			t.Fatalf("walk ads: %v", err)
		}
		if len(ads) < 3 {
			t.Fatalf("ads = %d, want >= 3", len(ads))
		}
		notesCid := updateRoots["notes.txt"]
		if !notesCid.Defined() {
			t.Fatalf("notes.txt not in roots: %+v", s.Roots)
		}
		for name, c := range step1Roots {
			if c == notesCid {
				t.Errorf("notes.txt CID equals step-1 root %s (%s); its content must differ", name, c)
			}
		}
		oldHello := step1Roots["hello.txt"]

		add := ads[0].Ad
		if add.IsRm || !bytes.Equal(add.ContextID, notesCid.Bytes()) {
			t.Errorf("head ad = rm:%v ctx:%x, want add for notes %x", add.IsRm, add.ContextID, notesCid.Bytes())
		}
		rm := ads[1].Ad
		if !rm.IsRm {
			t.Errorf("second ad IsRm = false, want true")
		}
		if rm.Entries != schema.NoEntries {
			t.Errorf("rm ad Entries = %v, want NoEntries", rm.Entries)
		}
		if !bytes.Equal(rm.ContextID, oldHello.Bytes()) {
			t.Errorf("rm ad ctx = %x, want old hello %x", rm.ContextID, oldHello.Bytes())
		}
		if ads[2].Cid.String() != step1.Head {
			t.Errorf("third ad = %s, want previous head %s", ads[2].Cid, step1.Head)
		}

		if resp, err := http.Get(baseURL + "/ipfs/" + oldHello.String()); err != nil {
			t.Fatalf("fetch old hello block: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("old hello block status = %d, want 404", resp.StatusCode)
			}
		}
		if resp, err := http.Get(baseURL + "/ipfs/" + step1Roots["photos"].String()); err != nil {
			t.Fatalf("fetch photos block: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("photos block status = %d, want 200", resp.StatusCode)
			}
		}
		// Re-read every block under the photos root over HTTP (each block's
		// multihash is verified by the link system).
		ls := httpLinkSystem(baseURL)
		photosCid := step1Roots["photos"]
		bigCid, err := dirChildLink(ctx, &ls, photosCid, "big.bin")
		if err != nil {
			t.Fatalf("photos/big.bin link after update: %v", err)
		}
		gotBig, err := readFileBytes(ctx, &ls, bigCid)
		if err != nil {
			t.Fatalf("read photos/big.bin after update: %v", err)
		}
		if !bytes.Equal(gotBig, bigContent) {
			t.Errorf("photos/big.bin bytes differ after update")
		}
		smallCid, err := dirChildLink(ctx, &ls, photosCid, "small.txt")
		if err != nil {
			t.Fatalf("photos/small.txt link after update: %v", err)
		}
		gotSmall, err := readFileBytes(ctx, &ls, smallCid)
		if err != nil {
			t.Fatalf("read photos/small.txt after update: %v", err)
		}
		if !bytes.Equal(gotSmall, smallContent) {
			t.Errorf("photos/small.txt bytes differ after update")
		}

		n := len(mustListAdFiles(t, siteDir))
		if n < adFileCount {
			t.Errorf("ad file count %d < previous %d", n, adFileCount)
		}
		adFileCount = n
	})

	t.Run("unpin all", func(t *testing.T) {
		entries, err := os.ReadDir(contentDir)
		if err != nil {
			t.Fatalf("read content: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if err := os.RemoveAll(filepath.Join(contentDir, e.Name())); err != nil {
				t.Fatalf("remove %s: %v", e.Name(), err)
			}
		}
		writeFile(t, filepath.Join(contentDir, ".gitkeep"), nil)

		code, stdout, stderr := runCLI(t, "publish", "--content", contentDir, "--site", siteDir, "--base-url", baseURL)
		if code != 0 {
			t.Fatalf("publish exit %d, stderr: %s", code, stderr)
		}
		s := decodeSummary(t, stdout)
		unpinHead = s.Head
		if !s.Changed {
			t.Errorf("changed = false, want true")
		}
		if len(s.Roots) != 0 {
			t.Errorf("roots = %d, want 0 (%+v)", len(s.Roots), s.Roots)
		}
		if !strings.Contains(stdout, `"roots":[]`) {
			t.Errorf("stdout missing empty roots: %s", stdout)
		}

		syncer, store := newSyncer(t, baseURL, id)
		head, err := syncer.GetHead(ctx)
		if err != nil {
			t.Fatalf("get head: %v", err)
		}
		ads, err := walkAds(ctx, syncer, store, head, id, mustBaseAddr(t, baseURL).String())
		if err != nil {
			t.Fatalf("walk ads: %v", err)
		}
		rmCtx := map[string]bool{}
		for _, a := range ads {
			if a.Ad.IsRm {
				rmCtx[string(a.Ad.ContextID)] = true
			}
		}
		for name, c := range updateRoots {
			if !rmCtx[string(c.Bytes())] {
				t.Errorf("no IsRm ad for remaining root %s (%s)", name, c)
			}
		}

		entries, err = os.ReadDir(filepath.Join(siteDir, "ipfs"))
		if err != nil {
			t.Fatalf("read ipfs dir: %v", err)
		}
		for _, e := range entries {
			if e.Name() != probeCID {
				t.Errorf("ipfs dir still holds %s, want only %s", e.Name(), probeCID)
			}
		}
		if fi, err := os.Stat(filepath.Join(siteDir, "ipfs", probeCID)); err != nil {
			t.Errorf("missing ipfs/%s after unpin: %v", probeCID, err)
		} else if fi.Size() != 0 {
			t.Errorf("ipfs/%s size = %d, want 0", probeCID, fi.Size())
		}

		n := len(mustListAdFiles(t, siteDir))
		if n < adFileCount {
			t.Errorf("ad file count %d < previous %d", n, adFileCount)
		}
		adFileCount = n
	})

	t.Run("re-run after unpinning everything", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "publish", "--content", contentDir, "--site", siteDir, "--base-url", baseURL)
		if code != 0 {
			t.Fatalf("publish exit %d, stderr: %s", code, stderr)
		}
		s := decodeSummary(t, stdout)
		if s.Changed {
			t.Errorf("changed = true, want false")
		}
		if !strings.Contains(stdout, `"changed":false`) {
			t.Errorf("stdout = %s, want it to contain %q", stdout, `"changed":false`)
		}
		if s.Head != unpinHead {
			t.Errorf("head = %s, want unchanged %s", s.Head, unpinHead)
		}
		if len(s.Roots) != 0 {
			t.Errorf("roots = %d, want 0 (%+v)", len(s.Roots), s.Roots)
		}

		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut && r.URL.Path == "/announce" {
				w.WriteHeader(http.StatusOK)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(fake.Close)

		code, stdout, stderr = runCLI(t, "announce", "--site", siteDir, "--base-url", baseURL,
			"--indexer", fake.URL, "--wait-served", "5s", "--every", "50ms")
		if code != 0 {
			t.Fatalf("announce exit %d, want 0 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stdout, "announced") {
			t.Errorf("announce stdout = %q, want it to mention announced", stdout)
		}
	})

	t.Run("first run with nothing", func(t *testing.T) {
		emptyContent := t.TempDir()
		freshSite := t.TempDir()
		code, _, stderr := runCLI(t, "publish", "--content", emptyContent, "--site", freshSite, "--base-url", baseURL)
		if code != 1 {
			t.Fatalf("exit = %d, want 1 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stderr, "no files in") {
			t.Errorf("stderr = %q, want it to contain %q", stderr, "no files in")
		}
	})

	// Steps 8 and 9 use the fresh site s2 served by the second static server.
	t.Run("announce", func(t *testing.T) {
		content2 := t.TempDir()
		writeFile(t, filepath.Join(content2, "one.txt"), []byte("the first s2 file\n"))
		writeFile(t, filepath.Join(content2, "two.txt"), []byte("the second s2 file, distinct bytes\n"))

		code, stdout, stderr := runCLI(t, "publish", "--content", content2, "--site", s2, "--base-url", baseURL2)
		if code != 0 {
			t.Fatalf("publish exit %d, stderr: %s", code, stderr)
		}
		s := decodeSummary(t, stdout)
		if len(s.Roots) < 2 {
			t.Fatalf("s2 roots = %d, want >= 2 (%+v)", len(s.Roots), s.Roots)
		}
		s2Head = s.Head
		for _, r := range s.Roots {
			c, err := cid.Decode(r.CID)
			if err != nil {
				t.Fatalf("s2 root cid: %v", err)
			}
			s2Roots[r.Name] = c
		}

		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut && r.URL.Path == "/announce" {
				body, _ := io.ReadAll(r.Body)
				announceMu.Lock()
				announced = append(announced, body)
				announceMu.Unlock()
				w.WriteHeader(http.StatusOK)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(fake.Close)

		code, stdout, stderr = runCLI(t, "announce", "--site", s2, "--base-url", baseURL2,
			"--indexer", fake.URL, "--wait-served", "5s", "--every", "50ms")
		if code != 0 {
			t.Fatalf("announce exit %d, stderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "announced") {
			t.Errorf("announce stdout = %q, want it to mention announced", stdout)
		}

		announceMu.Lock()
		var last []byte
		if len(announced) > 0 {
			last = announced[len(announced)-1]
		}
		announceMu.Unlock()
		if last == nil {
			t.Fatalf("fake indexer recorded no announce PUT")
		}
		var msg message.Message
		if err := msg.UnmarshalCBOR(bytes.NewReader(last)); err != nil {
			t.Fatalf("decode announce message: %v", err)
		}
		if msg.Cid.String() != s2Head {
			t.Errorf("announce cid = %s, want state head %s", msg.Cid, s2Head)
		}
		addrs, err := msg.GetAddrs()
		if err != nil {
			t.Fatalf("get addrs: %v", err)
		}
		if len(addrs) == 0 {
			t.Fatalf("announce message has no addrs")
		}
		prefix := fmt.Sprintf("/ip4/127.0.0.1/tcp/%s/http", portOf(t, baseURL2))
		if !strings.HasPrefix(addrs[0].String(), prefix) {
			t.Errorf("announce addr = %s, want prefix %s", addrs[0], prefix)
		}
		suffix := "/p2p/" + vectorPeerID
		if !strings.HasSuffix(addrs[0].String(), suffix) {
			t.Errorf("announce addr = %s, want suffix %s", addrs[0], suffix)
		}

		// Failure: the step-1 server serves a different chain, so the head is
		// never served there.
		announceMu.Lock()
		beforeFail := len(announced)
		announceMu.Unlock()

		code, _, stderr = runCLI(t, "announce", "--site", s2, "--base-url", baseURL,
			"--indexer", fake.URL, "--wait-served", "200ms", "--every", "50ms")
		if code != 1 {
			t.Fatalf("announce (wrong base) exit = %d, want 1 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stderr, "not serving the new head") {
			t.Errorf("stderr = %q, want it to contain %q", stderr, "not serving the new head")
		}
		announceMu.Lock()
		afterFail := len(announced)
		announceMu.Unlock()
		if afterFail != beforeFail {
			t.Errorf("fake indexer got %d announce PUTs after the failed wait, want 0", afterFail-beforeFail)
		}
	})

	t.Run("wait-indexed", func(t *testing.T) {
		if len(s2Roots) == 0 {
			t.Fatalf("s2 has no recorded roots; publish wrote none")
		}
		var queriedMu sync.Mutex
		queried := map[string]bool{}
		respondIndexed := func(w http.ResponseWriter, c cid.Cid, provider string) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"MultihashResults":[{"Multihash":%q,"ProviderResults":[{"Provider":{"ID":%q}}]}]}`,
				c.Hash().B58String(), provider)
		}
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/cid/") {
				cidStr := strings.TrimPrefix(r.URL.Path, "/cid/")
				c, err := cid.Decode(cidStr)
				if err != nil {
					http.Error(w, "bad cid", http.StatusBadRequest)
					return
				}
				queriedMu.Lock()
				queried[cidStr] = true
				queriedMu.Unlock()
				respondIndexed(w, c, vectorPeerID)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(fake.Close)

		code, stdout, stderr := runCLI(t, "wait-indexed", "--site", s2, "--indexer", fake.URL,
			"--timeout", "5s", "--every", "50ms")
		if code != 0 {
			t.Fatalf("wait-indexed exit = %d, want 0 (stderr: %s)", code, stderr)
		}
		for name, c := range s2Roots {
			want := fmt.Sprintf("indexed %s %s", name, c)
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout missing %q (got %q)", want, stdout)
			}
			queriedMu.Lock()
			got := queried[c.String()]
			queriedMu.Unlock()
			if !got {
				t.Errorf("indexer was never asked about root %s (%s)", name, c)
			}
		}

		// A root indexed only by some other provider is never found.
		names := make([]string, 0, len(s2Roots))
		for name := range s2Roots {
			names = append(names, name)
		}
		sort.Strings(names)
		wrongCid := s2Roots[names[0]]
		fakeWrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/cid/") {
				cidStr := strings.TrimPrefix(r.URL.Path, "/cid/")
				c, err := cid.Decode(cidStr)
				if err != nil {
					http.Error(w, "bad cid", http.StatusBadRequest)
					return
				}
				provider := vectorPeerID
				if c == wrongCid {
					provider = otherPeerID
				}
				respondIndexed(w, c, provider)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(fakeWrong.Close)

		code, _, stderr = runCLI(t, "wait-indexed", "--site", s2, "--indexer", fakeWrong.URL,
			"--timeout", "300ms", "--every", "50ms")
		if code != 2 {
			t.Fatalf("wait-indexed (wrong provider) exit = %d, want 2 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stderr, "not indexed yet") {
			t.Errorf("stderr = %q, want it to contain %q", stderr, "not indexed yet")
		}
		if !strings.Contains(stderr, wrongCid.String()) {
			t.Errorf("stderr = %q, want it to name the unindexed root %s", stderr, wrongCid)
		}

		fake404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		t.Cleanup(fake404.Close)

		code, _, stderr = runCLI(t, "wait-indexed", "--site", s2, "--indexer", fake404.URL,
			"--timeout", "300ms", "--every", "50ms")
		if code != 2 {
			t.Fatalf("wait-indexed (404) exit = %d, want 2 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stderr, "not indexed yet") {
			t.Errorf("stderr = %q, want it to contain %q", stderr, "not indexed yet")
		}
	})
}
