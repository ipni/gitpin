package dag

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	dagpb "github.com/ipld/go-codec-dagpb"
	"github.com/multiformats/go-multihash"
)

func TestSingleFile(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)

	content := []byte("Hello from my first IPFS pin!\n")
	os.WriteFile(filepath.Join(contentDir, "hello.txt"), content, 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	r := roots[0]
	if r.Name != "hello.txt" {
		t.Errorf("name = %q, want hello.txt", r.Name)
	}
	if r.CID.Prefix().Codec != cid.Raw {
		t.Errorf("codec = %d, want raw", r.CID.Prefix().Codec)
	}
	if r.CID.Version() != 1 {
		t.Errorf("version = %d, want 1", r.CID.Version())
	}
	if len(r.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(r.Blocks))
	}
	if r.Blocks[0] != r.CID {
		t.Errorf("block CID != root CID")
	}

	blockPath := filepath.Join(blocksDir, r.CID.String())
	data, err := os.ReadFile(blockPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedMH, _ := multihash.Sum(data, multihash.SHA2_256, -1)
	if !bytes.Equal(expectedMH, r.CID.Hash()) {
		t.Errorf("block hash mismatch")
	}
}

func TestDeterminism(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir1 := filepath.Join(tmp, "blocks1")
	blocksDir2 := filepath.Join(tmp, "blocks2")
	os.MkdirAll(contentDir, 0o755)
	os.WriteFile(filepath.Join(contentDir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(contentDir, "b.txt"), []byte("world"), 0o644)

	r1, err := Build(contentDir, blocksDir1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Build(contentDir, blocksDir2)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1) != len(r2) {
		t.Fatalf("different number of roots: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].CID != r2[i].CID {
			t.Errorf("root %d CID mismatch", i)
		}
		if len(r1[i].Blocks) != len(r2[i].Blocks) {
			t.Errorf("root %d blocks length mismatch", i)
		}
		for j := range r1[i].Blocks {
			if r1[i].Blocks[j] != r2[i].Blocks[j] {
				t.Errorf("root %d block %d mismatch", i, j)
			}
		}
	}
}

func TestLargeFile(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)

	rng := rand.New(rand.NewSource(42))
	data := make([]byte, 600*1024)
	for i := range data {
		data[i] = byte(rng.Intn(256))
	}
	os.WriteFile(filepath.Join(contentDir, "big.bin"), data, 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	r := roots[0]
	if r.CID.Prefix().Codec != cid.DagProtobuf {
		t.Errorf("codec = %d, want dag-pb", r.CID.Prefix().Codec)
	}
	if len(r.Blocks) != 4 {
		t.Fatalf("blocks = %d, want 4", len(r.Blocks))
	}
	for _, c := range r.Blocks {
		blockPath := filepath.Join(blocksDir, c.String())
		blockData, err := os.ReadFile(blockPath)
		if err != nil {
			t.Fatalf("block %s: %v", c, err)
		}
		expectedMH, _ := multihash.Sum(blockData, multihash.SHA2_256, -1)
		if !bytes.Equal(expectedMH, c.Hash()) {
			t.Errorf("block %s hash mismatch", c)
		}
	}
}

func TestDirectoryTree(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(filepath.Join(contentDir, "site", "sub"), 0o755)
	os.WriteFile(filepath.Join(contentDir, "site", "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(contentDir, "site", "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(contentDir, "site", ".hidden"), []byte("h"), 0o644)
	os.WriteFile(filepath.Join(contentDir, "site", "sub", "c.txt"), []byte("c"), 0o644)
	os.WriteFile(filepath.Join(contentDir, ".gitkeep"), []byte(""), 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	r := roots[0]
	if r.Name != "site" {
		t.Errorf("name = %q, want site", r.Name)
	}
	if r.CID.Prefix().Codec != cid.DagProtobuf {
		t.Errorf("codec = %d, want dag-pb", r.CID.Prefix().Codec)
	}

	blockData, err := os.ReadFile(filepath.Join(blocksDir, r.CID.String()))
	if err != nil {
		t.Fatal(err)
	}
	builder := dagpb.Type.PBNode.NewBuilder()
	if err := dagpb.DecodeBytes(builder, blockData); err != nil {
		t.Fatal(err)
	}
	node := builder.Build()
	pbn := node.(dagpb.PBNode)
	links := &pbn.Links
	if links.Length() != 3 {
		t.Fatalf("links = %d, want 3", links.Length())
	}
	var names []string
	for i := int64(0); i < links.Length(); i++ {
		link := links.Lookup(i)
		names = append(names, link.FieldName().Must().String())
	}
	want := []string{"a.txt", "b.txt", "sub"}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("link %d = %q, want %q", i, n, want[i])
		}
	}
}

func TestRootOrder(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	os.WriteFile(filepath.Join(contentDir, "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(contentDir, "a.txt"), []byte("a"), 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2", len(roots))
	}
	if roots[0].Name != "a.txt" || roots[1].Name != "b.txt" {
		t.Errorf("order = %q, %q; want a.txt, b.txt", roots[0].Name, roots[1].Name)
	}
}

func TestSharedBlocks(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	data := []byte("same content")
	os.WriteFile(filepath.Join(contentDir, "x.txt"), data, 0o644)
	os.WriteFile(filepath.Join(contentDir, "y.txt"), data, 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2", len(roots))
	}
	if roots[0].CID != roots[1].CID {
		t.Errorf("CIDs differ for identical content")
	}
	for _, r := range roots {
		found := false
		for _, b := range r.Blocks {
			if b == r.CID {
				found = true
			}
		}
		if !found {
			t.Errorf("root %s blocks missing root CID", r.Name)
		}
	}
}

func TestEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(filepath.Join(contentDir, "empty"), 0o755)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	if roots[0].Name != "empty" {
		t.Errorf("name = %q, want empty", roots[0].Name)
	}
	if roots[0].CID.Prefix().Codec != cid.DagProtobuf {
		t.Errorf("codec = %d, want dag-pb", roots[0].CID.Prefix().Codec)
	}
}

func TestSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on windows")
	}
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	os.WriteFile(filepath.Join(contentDir, "hello.txt"), []byte("hi"), 0o644)
	os.Symlink("hello.txt", filepath.Join(contentDir, "link"))

	_, err := Build(contentDir, blocksDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported file type: link") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestSymlinkInSubdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on windows")
	}
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(filepath.Join(contentDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(contentDir, "sub", "target.txt"), []byte("hi"), 0o644)
	os.Symlink("target.txt", filepath.Join(contentDir, "sub", "link"))

	_, err := Build(contentDir, blocksDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported file type: sub/link") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestMissingContentDir(t *testing.T) {
	tmp := t.TempDir()
	roots, err := Build(filepath.Join(tmp, "nonexistent"), filepath.Join(tmp, "blocks"))
	if err != nil {
		t.Fatal(err)
	}
	if roots == nil {
		t.Fatal("roots is nil")
	}
	if len(roots) != 0 {
		t.Fatalf("len = %d, want 0", len(roots))
	}
}

func TestOnlyDotfiles(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	os.WriteFile(filepath.Join(contentDir, ".gitkeep"), []byte(""), 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if roots == nil {
		t.Fatal("roots is nil")
	}
	if len(roots) != 0 {
		t.Fatalf("len = %d, want 0", len(roots))
	}
}

func TestMaxBlocksLimit(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	os.WriteFile(filepath.Join(contentDir, "a.txt"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(contentDir, "b.txt"), []byte("bbb"), 0o644)
	os.WriteFile(filepath.Join(contentDir, "c.txt"), []byte("ccc"), 0o644)

	orig := maxBlocks
	maxBlocks = 2
	t.Cleanup(func() { maxBlocks = orig })

	_, err := Build(contentDir, blocksDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "content has more than 50000 blocks") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestMaxTotalBytesLimit(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	os.WriteFile(filepath.Join(contentDir, "a.txt"), []byte("01234567890"), 0o644)

	orig := maxTotalBytes
	maxTotalBytes = 10
	t.Cleanup(func() { maxTotalBytes = orig })

	_, err := Build(contentDir, blocksDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "content is larger than 500 MiB") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestMaxBlocksSharedBlocks(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	data := []byte("same")
	os.WriteFile(filepath.Join(contentDir, "x.txt"), data, 0o644)
	os.WriteFile(filepath.Join(contentDir, "y.txt"), data, 0o644)

	orig := maxBlocks
	maxBlocks = 2
	t.Cleanup(func() { maxBlocks = orig })

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2", len(roots))
	}
}

func TestIdenticalLeaves(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)

	data := make([]byte, 512*1024)
	os.WriteFile(filepath.Join(contentDir, "zeros.bin"), data, 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	r := roots[0]
	if len(r.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(r.Blocks))
	}
	seen := make(map[cid.Cid]bool)
	for _, b := range r.Blocks {
		if seen[b] {
			t.Errorf("duplicate block %s", b)
		}
		seen[b] = true
	}
}

func TestEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	os.MkdirAll(contentDir, 0o755)
	os.WriteFile(filepath.Join(contentDir, "empty.txt"), []byte(""), 0o644)

	roots, err := Build(contentDir, blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	r := roots[0]
	if r.CID.Prefix().Codec != cid.Raw {
		t.Errorf("codec = %d, want raw", r.CID.Prefix().Codec)
	}
	if len(r.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(r.Blocks))
	}
}
