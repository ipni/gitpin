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
	base := strings.TrimRight(baseURL, "/")
	u := base + "/ipni/v1/ad/head"
	last := "no response"
	if c == nil {
		c = http.DefaultClient
	}
	noredirect := *c
	noredirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	c = &noredirect
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("build request for %s: %w", u, err)
		}
		resp, err := c.Do(req)
		if err == nil {
			if isRedirect(resp.StatusCode) {
				loc := redirectLocation(resp)
				resp.Body.Close()
				return redirectErr(u, loc)
			}
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
			resp.Body.Close()
			last = fmt.Sprintf("%d", resp.StatusCode)
			if rerr == nil && resp.StatusCode == http.StatusOK && bytes.Equal(body, want) {
				return nil
			}
		} else if ctx.Err() == nil {
			last = "no response"
		}
		select {
		case <-ctx.Done():
			return notServedErr(u, last, base, ctx.Err())
		case <-time.After(every):
		}
	}
}

// isRedirect reports whether status is one IPFS clients treat as "not available".
func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// redirectLocation returns the resolved Location header of a redirect response.
func redirectLocation(resp *http.Response) string {
	if loc, err := resp.Location(); err == nil {
		return loc.String()
	}
	return resp.Header.Get("Location")
}

// redirectErr is the error WaitServed returns at once when the site redirects.
func redirectErr(u, loc string) error {
	origin := ""
	if lu, err := url.Parse(loc); err == nil && lu.Host != "" {
		origin = lu.Scheme + "://" + lu.Host
	}
	if origin == "" {
		return fmt.Errorf("%s redirects to %s; IPFS clients do not follow redirects, so set base-url to the site that serves the files directly", u, loc)
	}
	return fmt.Errorf("%s redirects to %s; IPFS clients do not follow redirects, so set base-url to the site that serves the files directly (for example %s)", u, loc, origin)
}

// notServedErr is the single error WaitServed reports when the head never appears.
func notServedErr(u, last, baseURL string, err error) error {
	return fmt.Errorf("%s is not serving the new head yet (last status %s); check that GitHub Pages is on for this repo with source \"Deploy from a branch\" and the site branch (gh-pages by default), that the repo is public, and that the site is served at %s: %w", u, last, baseURL, err)
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
		if err != nil {
			return fmt.Errorf("build request for %s: %w", u, err)
		}
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
							ContextID []byte
						}
					}
				}
				if json.Unmarshal(body, &result) == nil {
					for _, mr := range result.MultihashResults {
						for _, pr := range mr.ProviderResults {
							if pr.Provider.ID == id.String() && bytes.Equal(pr.ContextID, root.Bytes()) {
								return nil
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
