// Package dag builds UnixFS DAGs for the files in a content directory.
package dag

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-unixfsnode/data/builder"
	dagpb "github.com/ipld/go-codec-dagpb"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
)

// Chunker names the chunking strategy used when building file DAGs.
const Chunker = "size-262144"

// MaxTotalBytes is the maximum total content size a build may process.
const MaxTotalBytes = 500 << 20

// MaxBlocks is the maximum number of blocks a single build may produce.
const MaxBlocks = 50000

var (
	maxTotalBytes = MaxTotalBytes
	maxBlocks     = MaxBlocks
)

// Root is a built content root and the blocks that back it.
type Root struct {
	Name   string
	CID    cid.Cid
	Blocks []cid.Cid
}

// Build builds a UnixFS DAG for each file in contentDir, writing blocks to blocksDir.
func Build(contentDir, blocksDir string) ([]Root, error) {
	entries, err := os.ReadDir(contentDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Root{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", contentDir, err)
	}

	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return []Root{}, nil
	}

	if err := os.MkdirAll(blocksDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", blocksDir, err)
	}

	var cur map[cid.Cid]struct{}
	global := make(map[cid.Cid]struct{})
	var totalBytes int64

	ls := cidlink.DefaultLinkSystem()
	ls.StorageWriteOpener = func(lctx ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		buf := &bytes.Buffer{}
		return buf, func(lnk ipld.Link) error {
			cl, ok := lnk.(cidlink.Link)
			if !ok {
				return fmt.Errorf("unexpected link type %T", lnk)
			}
			c := cl.Cid
			data := buf.Bytes()
			if _, exists := global[c]; !exists {
				global[c] = struct{}{}
				totalBytes += int64(len(data))
				if totalBytes > int64(maxTotalBytes) {
					return errors.New("content is larger than 500 MiB")
				}
				if len(global) > maxBlocks {
					return errors.New("content has more than 50000 blocks")
				}
			}
			if cur != nil {
				cur[c] = struct{}{}
			}
			return os.WriteFile(filepath.Join(blocksDir, c.String()), data, 0o644)
		}, nil
	}

	var roots []Root
	for _, name := range names {
		cur = make(map[cid.Cid]struct{})
		link, _, err := buildPath(filepath.Join(contentDir, name), name, &ls)
		if err != nil {
			return nil, err
		}
		cl, ok := link.(cidlink.Link)
		if !ok {
			return nil, fmt.Errorf("unexpected link type %T", link)
		}
		blocks := make([]cid.Cid, 0, len(cur))
		for c := range cur {
			blocks = append(blocks, c)
		}
		sort.Slice(blocks, func(i, j int) bool {
			return blocks[i].String() < blocks[j].String()
		})
		roots = append(roots, Root{Name: name, CID: cl.Cid, Blocks: blocks})
	}
	return roots, nil
}

func buildPath(abs, rel string, ls *ipld.LinkSystem) (ipld.Link, uint64, error) {
	info, err := os.Lstat(abs)
	if err != nil {
		return nil, 0, fmt.Errorf("lstat %s: %w", rel, err)
	}
	switch {
	case info.Mode().IsRegular():
		f, err := os.Open(abs)
		if err != nil {
			return nil, 0, fmt.Errorf("open %s: %w", rel, err)
		}
		defer f.Close()
		link, size, err := builder.BuildUnixFSFile(f, Chunker, ls)
		if err != nil {
			return nil, 0, fmt.Errorf("build file %s: %w", rel, err)
		}
		return link, size, nil
	case info.Mode().IsDir():
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, 0, fmt.Errorf("read %s: %w", rel, err)
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		var pblinks []dagpb.PBLink
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			childRel := rel + "/" + e.Name()
			childLink, size, err := buildPath(filepath.Join(abs, e.Name()), childRel, ls)
			if err != nil {
				return nil, 0, err
			}
			pbl, err := builder.BuildUnixFSDirectoryEntry(e.Name(), int64(size), childLink)
			if err != nil {
				return nil, 0, fmt.Errorf("entry %s: %w", childRel, err)
			}
			pblinks = append(pblinks, pbl)
		}
		link, size, err := builder.BuildUnixFSDirectory(pblinks, ls)
		if err != nil {
			return nil, 0, fmt.Errorf("build dir %s: %w", rel, err)
		}
		return link, size, nil
	default:
		return nil, 0, fmt.Errorf("unsupported file type: %s", filepath.ToSlash(rel))
	}
}
