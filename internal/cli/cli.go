// Package cli implements the gitpin command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipni/gitpin/internal/announce"
	"github.com/ipni/gitpin/internal/dag"
	"github.com/ipni/gitpin/internal/ipni"
	"github.com/ipni/gitpin/internal/keys"
	"github.com/ipni/gitpin/internal/site"
	"github.com/ipni/gitpin/internal/state"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	exitOK         = 0
	exitErr        = 1
	exitNotIndexed = 2
	exitUsage      = 64
)

// summaryRoot is the per-root entry in the publish command's JSON summary.
type summaryRoot struct {
	Name string `json:"name"`
	CID  string `json:"cid"`
}

// Run dispatches a gitpin subcommand and returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "keygen":
		return runKeygen(args[1:], stdout, stderr)
	case "publish":
		return runPublish(ctx, args[1:], stdout, stderr)
	case "announce":
		return runAnnounce(ctx, args[1:], stdout, stderr)
	case "wait-indexed":
		return runWaitIndexed(ctx, args[1:], stdout, stderr)
	default:
		usage(stderr)
		return exitUsage
	}
}

// usage prints the top-level usage text listing the four subcommands.
func usage(stderr io.Writer) {
	fmt.Fprint(stderr, `usage: gitpin <command> [flags]

commands:
  keygen        generate a signing key and print GITPIN_KEY and PEER_ID
  publish       build content and publish advertisements to the site directory
  announce      announce the published chain head to an indexer
  wait-indexed  wait for the published roots to be indexed
`)
}

// newFlagSet builds a subcommand flag set that prints errors to stderr and
// suppresses the flag package's own usage output.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {}
	return fs
}

// runKeygen generates a signing key and prints it with the derived peer ID.
func runKeygen(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("keygen", stderr)
	if err := fs.Parse(args); err != nil {
		usage(stderr)
		return exitUsage
	}
	if fs.NArg() != 0 {
		usage(stderr)
		return exitUsage
	}
	k, b64, err := keys.Generate()
	if err != nil {
		return fail(stderr, err)
	}
	id, _ := peer.IDFromPrivateKey(k)
	fmt.Fprintf(stdout, "GITPIN_KEY=%s\nPEER_ID=%s\n", b64, id)
	return exitOK
}

// runPublish builds the content DAGs, publishes advertisements, assembles the
// site, saves state, and prints a JSON summary.
func runPublish(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("publish", stderr)
	contentDir := fs.String("content", "", "content directory")
	siteDir := fs.String("site", "", "site directory")
	baseURL := fs.String("base-url", "", "public base URL")
	if err := fs.Parse(args); err != nil {
		usage(stderr)
		return exitUsage
	}
	if fs.NArg() != 0 {
		usage(stderr)
		return exitUsage
	}
	if *contentDir == "" || *siteDir == "" || *baseURL == "" {
		usage(stderr)
		return exitUsage
	}
	key, err := keys.FromEnv()
	if err != nil {
		return fail(stderr, err)
	}
	addr, err := site.BaseAddr(*baseURL)
	if err != nil {
		return fail(stderr, err)
	}
	prev, err := state.Load(*siteDir)
	if err != nil {
		return fail(stderr, err)
	}
	roots, err := dag.Build(*contentDir, filepath.Join(*siteDir, "ipfs"))
	if err != nil {
		return fail(stderr, err)
	}
	if len(roots) == 0 && prev.Head == "" {
		return failf(stderr, "no files in %s: add a file to it and push again", *contentDir)
	}
	next, changed, err := ipni.Publish(ipni.Params{
		Key:     key,
		Addr:    addr,
		BaseURL: normalizeBaseURL(*baseURL),
		AdDir:   filepath.Join(*siteDir, "ipni", "v1", "ad"),
	}, prev, roots)
	if err != nil {
		return fail(stderr, err)
	}
	if err := site.Assemble(*siteDir, next, roots); err != nil {
		return fail(stderr, err)
	}
	if err := state.Save(*siteDir, next); err != nil {
		return fail(stderr, err)
	}
	summary := make([]summaryRoot, 0, len(next.Roots))
	for _, r := range next.Roots {
		summary = append(summary, summaryRoot{Name: r.Name, CID: r.CID})
	}
	out := struct {
		Changed bool          `json:"changed"`
		Head    string        `json:"head"`
		PeerID  string        `json:"peer_id"`
		Roots   []summaryRoot `json:"roots"`
	}{
		Changed: changed,
		Head:    next.Head,
		PeerID:  next.PeerID,
		Roots:   summary,
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// runAnnounce waits for the chain head to be served and announces it.
func runAnnounce(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("announce", stderr)
	siteDir := fs.String("site", "", "site directory")
	baseURL := fs.String("base-url", "", "public base URL")
	indexerURL := fs.String("indexer", "https://cid.contact", "indexer URL")
	waitServed := fs.Duration("wait-served", 10*time.Minute, "wait for the gateway to serve the head")
	every := fs.Duration("every", 10*time.Second, "poll interval")
	if err := fs.Parse(args); err != nil {
		usage(stderr)
		return exitUsage
	}
	if fs.NArg() != 0 {
		usage(stderr)
		return exitUsage
	}
	if *siteDir == "" || *baseURL == "" {
		usage(stderr)
		return exitUsage
	}
	st, err := state.Load(*siteDir)
	if err != nil {
		return fail(stderr, err)
	}
	if st.Head == "" {
		return failf(stderr, "nothing to announce: run publish first")
	}
	id, err := peer.Decode(st.PeerID)
	if err != nil {
		return fail(stderr, err)
	}
	head, err := cid.Parse(st.Head)
	if err != nil {
		return fail(stderr, err)
	}
	addr, err := site.BaseAddr(*baseURL)
	if err != nil {
		return fail(stderr, err)
	}
	base := normalizeBaseURL(*baseURL)
	if *waitServed > 0 {
		want, err := os.ReadFile(filepath.Join(*siteDir, "ipni", "v1", "ad", "head"))
		if err != nil {
			return fail(stderr, err)
		}
		wctx, cancel := context.WithTimeout(ctx, *waitServed)
		defer cancel()
		if err := announce.WaitServed(wctx, &http.Client{Timeout: 30 * time.Second}, base, want, *every); err != nil {
			return fail(stderr, err)
		}
	}
	if err := announce.Send(ctx, *indexerURL, id, head, addr); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "announced %s to %s\n", st.Head, *indexerURL)
	return exitOK
}

// runWaitIndexed waits for each published root to be indexed.
func runWaitIndexed(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("wait-indexed", stderr)
	siteDir := fs.String("site", "", "site directory")
	indexerURL := fs.String("indexer", "https://cid.contact", "indexer URL")
	timeout := fs.Duration("timeout", 15*time.Minute, "overall timeout")
	every := fs.Duration("every", 15*time.Second, "poll interval")
	if err := fs.Parse(args); err != nil {
		usage(stderr)
		return exitUsage
	}
	if fs.NArg() != 0 {
		usage(stderr)
		return exitUsage
	}
	if *siteDir == "" {
		usage(stderr)
		return exitUsage
	}
	st, err := state.Load(*siteDir)
	if err != nil {
		return fail(stderr, err)
	}
	if len(st.Roots) == 0 {
		return exitOK
	}
	id, err := peer.Decode(st.PeerID)
	if err != nil {
		return fail(stderr, err)
	}
	tctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	for _, r := range st.Roots {
		root, err := cid.Parse(r.CID)
		if err != nil {
			return fail(stderr, err)
		}
		if err := announce.WaitIndexed(tctx, &http.Client{Timeout: 30 * time.Second}, *indexerURL, root, id, *every); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				fmt.Fprintf(stderr, "gitpin: not indexed yet: %s (cid.contact can take a while; try again later)\n", r.CID)
				return exitNotIndexed
			}
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "indexed %s %s\n", r.Name, r.CID)
	}
	return exitOK
}

// normalizeBaseURL lowercases the scheme and host and drops one trailing slash
// from the path of a base URL that site.BaseAddr has already accepted, so
// https://Alice.github.io and https://alice.github.io/ name the same site.
func normalizeBaseURL(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimSuffix(baseURL, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "https" && u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

// fail prints a gitpin-prefixed error to stderr and returns the error exit code.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "gitpin: %v\n", err)
	return exitErr
}

// failf prints a formatted gitpin-prefixed error to stderr and returns the error exit code.
func failf(stderr io.Writer, format string, a ...any) int {
	fmt.Fprintf(stderr, "gitpin: %s\n", fmt.Sprintf(format, a...))
	return exitErr
}
