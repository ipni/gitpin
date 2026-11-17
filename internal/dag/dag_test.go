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

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestSingleFile(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	mkdirAll(t, contentDir)

	content := []byte("Hello from my first IPFS pin!\n")
	writeFile(t, filepath.Join(contentDir, "hello.txt"), content)

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
	mkdirAll(t, filepath.Join(contentDir, "sub"))
	writeFile(t, filepath.Join(contentDir, "a.txt"), []byte("hello"))
	writeFile(t, filepath.Join(contentDir, "b.txt"), []byte("world"))
	writeFile(t, filepath.Join(contentDir, "sub", "c.txt"), []byte("nested"))

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
			t.Fatalf("root %d blocks length mismatch", i)
		}
		for j := range r1[i].Blocks {
			if r1[i].Blocks[j] != r2[i].Blocks[j] {
				t.Errorf("root %d block %d mismatch", i, j)
			}
			name := r1[i].Blocks[j].String()
			d1, err := os.ReadFile(filepath.Join(blocksDir1, name))
			if err != nil {
				t.Fatalf("blocksDir1/%s: %v", name, err)
			}
			d2, err := os.ReadFile(filepath.Join(blocksDir2, name))
			if err != nil {
				t.Fatalf("blocksDir2/%s: %v", name, err)
			}
			if !bytes.Equal(d1, d2) {
				t.Errorf("block %s content differs between dirs", name)
			}
		}
	}
}

func TestLargeFile(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	mkdirAll(t, contentDir)

	rng := rand.New(rand.NewSource(42))
	data := make([]byte, 600*1024)
	for i := range data {
		data[i] = byte(rng.Intn(256))
	}
	writeFile(t, filepath.Join(contentDir, "big.bin"), data)

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
	mkdirAll(t, filepath.Join(contentDir, "site", "sub"))
	writeFile(t, filepath.Join(contentDir, "site", "b.txt"), []byte("b"))
	writeFile(t, filepath.Join(contentDir, "site", "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(contentDir, "site", ".hidden"), []byte("h"))
	writeFile(t, filepath.Join(contentDir, "site", "sub", "c.txt"), []byte("c"))
	writeFile(t, filepath.Join(contentDir, "site", "sub", ".dotfile"), []byte("d"))
	writeFile(t, filepath.Join(contentDir, ".gitkeep"), []byte(""))

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
	bldr := dagpb.Type.PBNode.NewBuilder()
	if err := dagpb.DecodeBytes(bldr, blockData); err != nil {
		t.Fatal(err)
	}
	node := bldr.Build()
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

	for _, c := range r.Blocks {
		if c.String() == "" {
			t.Fatal("empty CID in blocks")
		}
	}
	entries, err := os.ReadDir(blocksDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("dotfile block written: %s", e.Name())
		}
	}
}

func TestRootOrder(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	mkdirAll(t, contentDir)
	writeFile(t, filepath.Join(contentDir, "b.txt"), []byte("b"))
	writeFile(t, filepath.Join(contentDir, "a.txt"), []byte("a"))

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
	mkdirAll(t, contentDir)
	data := []byte("same content")
	writeFile(t, filepath.Join(contentDir, "x.txt"), data)
	writeFile(t, filepath.Join(contentDir, "y.txt"), data)

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
		if len(r.Blocks) != 1 {
			t.Fatalf("root %s: blocks = %d, want 1", r.Name, len(r.Blocks))
		}
		if r.Blocks[0] != r.CID {
			t.Errorf("root %s: block[0] != CID", r.Name)
		}
	}
}

func TestEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	mkdirAll(t, filepath.Join(contentDir, "empty"))

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
	mkdirAll(t, contentDir)
	writeFile(t, filepath.Join(contentDir, "hello.txt"), []byte("hi"))
	if err := os.Symlink("hello.txt", filepath.Join(contentDir, "link")); err != nil {
		t.Fatal(err)
	}

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
	mkdirAll(t, filepath.Join(contentDir, "sub"))
	writeFile(t, filepath.Join(contentDir, "sub", "target.txt"), []byte("hi"))
	if err := os.Symlink("target.txt", filepath.Join(contentDir, "sub", "link")); err != nil {
		t.Fatal(err)
	}

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
	mkdirAll(t, contentDir)
	writeFile(t, filepath.Join(contentDir, ".gitkeep"), []byte(""))

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
	mkdirAll(t, contentDir)
	writeFile(t, filepath.Join(contentDir, "a.txt"), []byte("aaa"))
	writeFile(t, filepath.Join(contentDir, "b.txt"), []byte("bbb"))
	writeFile(t, filepath.Join(contentDir, "c.txt"), []byte("ccc"))

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
	mkdirAll(t, contentDir)
	writeFile(t, filepath.Join(contentDir, "a.txt"), []byte("01234567890"))

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
	mkdirAll(t, contentDir)
	data := []byte("same")
	writeFile(t, filepath.Join(contentDir, "x.txt"), data)
	writeFile(t, filepath.Join(contentDir, "y.txt"), data)

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
	mkdirAll(t, contentDir)

	data := make([]byte, 512*1024)
	writeFile(t, filepath.Join(contentDir, "zeros.bin"), data)

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
	hasRoot := false
	hasRaw := false
	for _, b := range r.Blocks {
		if b == r.CID {
			hasRoot = true
		}
		if b.Prefix().Codec == cid.Raw {
			hasRaw = true
		}
		blockPath := filepath.Join(blocksDir, b.String())
		if _, err := os.Stat(blockPath); err != nil {
			t.Errorf("block file %s missing: %v", b, err)
		}
	}
	if !hasRoot {
		t.Error("Blocks does not contain root CID")
	}
	if !hasRaw {
		t.Error("Blocks does not contain a raw block")
	}
}

func TestEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	contentDir := filepath.Join(tmp, "content")
	blocksDir := filepath.Join(tmp, "blocks")
	mkdirAll(t, contentDir)
	writeFile(t, filepath.Join(contentDir, "empty.txt"), []byte(""))

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
