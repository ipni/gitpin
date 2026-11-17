// Package announce waits for content to be served and announces the chain head to indexers.
package announce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/announce/httpsender"
	"github.com/ipni/go-libipni/announce/message"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// WaitServed polls the gateway until the want bytes are served or ctx ends.
func WaitServed(ctx context.Context, c *http.Client, baseURL string, want []byte, every time.Duration) error {
	u := strings.TrimRight(baseURL, "/") + "/ipni/v1/ad/head"
	last := "no response"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("%s is not serving the new head yet (last status %s); check that GitHub Pages is on for this repo with source \"Deploy from a branch\" and branch gh-pages, that the repo is public, and that it is named <owner>.github.io: %w", u, last, ctx.Err())
		}
		resp, err := c.Do(req)
		if err == nil {
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
			resp.Body.Close()
			last = fmt.Sprintf("%d", resp.StatusCode)
			if rerr == nil && resp.StatusCode == http.StatusOK && bytes.Equal(body, want) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s is not serving the new head yet (last status %s); check that GitHub Pages is on for this repo with source \"Deploy from a branch\" and branch gh-pages, that the repo is public, and that it is named <owner>.github.io: %w", u, last, ctx.Err())
		case <-time.After(every):
		}
	}
}

// Send announces the chain head to an indexer.
func Send(ctx context.Context, indexerURL string, id peer.ID, head cid.Cid, addr multiaddr.Multiaddr) error {
	u, err := url.Parse(indexerURL)
	if err != nil {
		return fmt.Errorf("announce to %s: %w", indexerURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("announce to %s: unsupported scheme %q", indexerURL, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("announce to %s: empty host", indexerURL)
	}
	if u.Path == "/" {
		u.Path = ""
	}
	sender, err := httpsender.New([]*url.URL{u}, id,
		httpsender.WithTimeout(30*time.Second),
		httpsender.WithUserAgent("gitpin"),
	)
	if err != nil {
		return fmt.Errorf("announce to %s: %w", indexerURL, err)
	}
	defer sender.Close()
	msg := message.Message{Cid: head}
	msg.SetAddrs([]multiaddr.Multiaddr{addr})
	if err := sender.Send(ctx, msg); err != nil {
		return fmt.Errorf("announce to %s: %w", indexerURL, err)
	}
	return nil
}

// WaitIndexed polls an indexer until root is indexed or ctx ends.
func WaitIndexed(ctx context.Context, c *http.Client, indexerURL string, root cid.Cid, id peer.ID, every time.Duration) error {
	u := strings.TrimRight(indexerURL, "/") + "/cid/" + root.String()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err == nil {
			req.Header.Set("Accept", "application/json")
			resp, rerr := c.Do(req)
			if rerr == nil {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20+1))
				drainAndClose(resp)
				if resp.StatusCode == http.StatusOK {
					var result struct {
						MultihashResults []struct {
							ProviderResults []struct {
								Provider struct {
									ID string
								}
							}
						}
					}
					if json.Unmarshal(body, &result) == nil {
						for _, mr := range result.MultihashResults {
							for _, pr := range mr.ProviderResults {
								if pr.Provider.ID == id.String() {
									return nil
								}
							}
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("not indexed yet: %s: %w", root, ctx.Err())
		case <-time.After(every):
		}
	}
}

func drainAndClose(resp *http.Response) {
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
}
