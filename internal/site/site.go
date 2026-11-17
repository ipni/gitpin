// Package site assembles the static gh-pages site layout for a gitpin run.
package site

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ipni/gitpin/internal/dag"
	"github.com/ipni/gitpin/internal/state"
	"github.com/multiformats/go-multiaddr"
)

// ProbeCID is the CID of the empty payload used to probe the gateway.
const ProbeCID = "bafkqaaa"

//go:embed index.html.tmpl
var indexHTML string

var indexTmpl = template.Must(template.New("index.html").Parse(indexHTML))

// BaseAddr converts a site base URL into a libp2p HTTP gateway multiaddr.
func BaseAddr(baseURL string) (multiaddr.Multiaddr, error) {
	httpsErr := fmt.Errorf("base URL must use https: %s", baseURL)
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, httpsErr
	}
	host := u.Hostname()
	switch u.Scheme {
	case "https":
		if host == "" || net.ParseIP(host) != nil {
			return nil, httpsErr
		}
	case "http":
		h := strings.ToLower(host)
		if h != "127.0.0.1" && h != "localhost" {
			return nil, httpsErr
		}
	default:
		return nil, httpsErr
	}
	if u.User != nil {
		return nil, httpsErr
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(baseURL, "#") {
		return nil, httpsErr
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("base URL must be a site root like https://alice.github.io, not %s (project pages under a path are not supported)", baseURL)
	}
	port := u.Port()
	if u.Scheme == "https" {
		if port == "" {
			port = "443"
		}
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return nil, httpsErr
		}
		return multiaddr.NewMultiaddr(fmt.Sprintf("/dns/%s/tcp/%d/https", strings.ToLower(host), p))
	}
	if port == "" {
		return nil, httpsErr
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return nil, httpsErr
	}
	return multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/http", p))
}

// Assemble writes the static site files for the given state and roots.
func Assemble(siteDir string, st state.State, roots []dag.Root) error {
	ipfsDir := filepath.Join(siteDir, "ipfs")
	if err := os.MkdirAll(ipfsDir, 0o755); err != nil {
		return fmt.Errorf("site: %w", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, ".nojekyll"), nil, 0o644); err != nil {
		return fmt.Errorf("site: %w", err)
	}
	if err := os.WriteFile(filepath.Join(ipfsDir, ProbeCID), nil, 0o644); err != nil {
		return fmt.Errorf("site: %w", err)
	}
	keep := map[string]struct{}{ProbeCID: {}}
	for _, r := range roots {
		for _, b := range r.Blocks {
			keep[b.String()] = struct{}{}
		}
	}
	entries, err := os.ReadDir(ipfsDir)
	if err != nil {
		return fmt.Errorf("site: %w", err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if _, ok := keep[e.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(ipfsDir, e.Name())); err != nil {
			return fmt.Errorf("site: %w", err)
		}
	}
	var buf bytes.Buffer
	if err := indexTmpl.Execute(&buf, st); err != nil {
		return fmt.Errorf("site: %w", err)
	}
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("site: %w", err)
	}
	return nil
}
