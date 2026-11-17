package announce

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/announce/message"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

const every = 10 * time.Millisecond

func testID(t *testing.T) peer.ID {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	priv, err := crypto.UnmarshalEd25519PrivateKey(mustUnmarshalSeed(t, seed))
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustUnmarshalSeed(t *testing.T, seed [32]byte) []byte {
	t.Helper()
	return ed25519.NewKeyFromSeed(seed[:])
}

func testCid(t *testing.T) cid.Cid {
	t.Helper()
	d := sha256.Sum256([]byte("gitpin test"))
	p := cid.Prefix{Version: 1, Codec: 0x71, MhType: 0x12, MhLength: 32}
	c, err := p.Sum(d[:])
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestWaitServed(t *testing.T) {
	var mu sync.Mutex
	count := 0
	var paths []string
	want := []byte("signed head bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		count++
		paths = append(paths, r.URL.RequestURI())
		switch count {
		case 1, 2:
			w.WriteHeader(http.StatusNotFound)
		case 3:
			w.Write([]byte("other bytes"))
		default:
			w.Write(want)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitServed(ctx, srv.Client(), srv.URL, want, every); err != nil {
		t.Fatalf("WaitServed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 4 {
		t.Fatalf("want 4 requests, got %d", count)
	}
	for i, p := range paths {
		if p != "/ipni/v1/ad/head" {
			t.Errorf("request %d path %q", i, p)
		}
	}
}

func TestWaitServedTrailingSlash(t *testing.T) {
	var mu sync.Mutex
	var path string
	want := []byte("head")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path = r.URL.RequestURI()
		w.Write(want)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitServed(ctx, srv.Client(), srv.URL+"/", want, every); err != nil {
		t.Fatalf("WaitServed: %v", err)
	}
	if path != "/ipni/v1/ad/head" {
		t.Fatalf("path %q", path)
	}
}

func TestWaitServedTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := WaitServed(ctx, srv.Client(), srv.URL, []byte("nope"), every)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(err.Error(), "last status 404") {
		t.Errorf("err %q lacks last status 404", err)
	}
	if !strings.Contains(err.Error(), "gh-pages") {
		t.Errorf("err %q lacks gh-pages", err)
	}
}

func TestWaitServedCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(50*time.Millisecond, cancel)
	defer timer.Stop()
	start := time.Now()
	err := WaitServed(ctx, srv.Client(), srv.URL, []byte("nope"), every)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("WaitServed took %s, want prompt return", elapsed)
	}
}

func TestWaitServedCancelledKeepsLastStatus(t *testing.T) {
	var mu sync.Mutex
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := WaitServed(ctx, srv.Client(), srv.URL, []byte("nope"), every)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(err.Error(), "last status 404") {
		t.Errorf("err %q lacks last status 404", err)
	}
}

func TestWaitServedWrongBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("other bytes"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := WaitServed(ctx, srv.Client(), srv.URL, []byte("want"), every)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "is not serving the new head yet") {
		t.Errorf("err %q lacks 'is not serving the new head yet'", err)
	}
	if !strings.Contains(err.Error(), "last status 200") {
		t.Errorf("err %q lacks last status 200", err)
	}
}

func TestWaitServedBuildRequestError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := WaitServed(ctx, http.DefaultClient, "http://exa mple.com", []byte("want"), every)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "build request for") {
		t.Errorf("err %q, want it to mention building the request", err)
	}
	if strings.Contains(err.Error(), "<nil>") {
		t.Errorf("err %q, want it not to wrap a nil context error", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("err %q, want it not to be a context error", err)
	}
	if elapsed > time.Second {
		t.Errorf("WaitServed took %s, want it to fail at once", elapsed)
	}
}

func TestSend(t *testing.T) {
	id := testID(t)
	head := testCid(t)
	addr, err := multiaddr.NewMultiaddr("/dns/alice.github.io/tcp/443/https")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var gotMethod, gotPath, gotCT, gotUA string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		gotBody = body
		mu.Unlock()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Send(ctx, srv.URL, id, head, addr); err != nil {
		t.Fatalf("Send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotMethod != http.MethodPut {
		t.Errorf("method %q", gotMethod)
	}
	if gotPath != "/announce" {
		t.Errorf("path %q", gotPath)
	}
	if gotCT != "application/octet-stream" {
		t.Errorf("content type %q", gotCT)
	}
	if gotUA != "gitpin" {
		t.Errorf("user agent %q", gotUA)
	}
	var msg message.Message
	if err := msg.UnmarshalCBOR(bytes.NewReader(gotBody)); err != nil {
		t.Fatalf("UnmarshalCBOR: %v", err)
	}
	if msg.Cid != head {
		t.Errorf("cid %s != %s", msg.Cid, head)
	}
	addrs, err := msg.GetAddrs()
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 {
		t.Fatalf("addrs %d", len(addrs))
	}
	s := addrs[0].String()
	if !strings.HasPrefix(s, "/dns/alice.github.io/tcp/443/https") {
		t.Errorf("addr %q", s)
	}
	if !strings.HasSuffix(s, "/p2p/"+id.String()) {
		t.Errorf("addr %q", s)
	}
}

func TestSendTrailingSlash(t *testing.T) {
	id := testID(t)
	head := testCid(t)
	addr, _ := multiaddr.NewMultiaddr("/dns/alice.github.io/tcp/443/https")
	var mu sync.Mutex
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Send(ctx, srv.URL+"/", id, head, addr); err != nil {
		t.Fatalf("Send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/announce" {
		t.Errorf("path %q", gotPath)
	}
}

func TestSend500(t *testing.T) {
	id := testID(t)
	head := testCid(t)
	addr, _ := multiaddr.NewMultiaddr("/dns/alice.github.io/tcp/443/https")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Send(ctx, srv.URL, id, head, addr); err == nil {
		t.Fatal("want error")
	}
}

func TestSendBadURLs(t *testing.T) {
	id := testID(t)
	head := testCid(t)
	addr, _ := multiaddr.NewMultiaddr("/dns/alice.github.io/tcp/443/https")
	for _, u := range []string{"::bad", "ftp://x"} {
		if err := Send(context.Background(), u, id, head, addr); err == nil {
			t.Errorf("Send(%q): want error", u)
		}
	}
}

func TestWaitIndexed(t *testing.T) {
	id := testID(t)
	root := testCid(t)
	var mu sync.Mutex
	count := 0
	var path, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		path = r.URL.Path
		accept = r.Header.Get("Accept")
		n := count
		mu.Unlock()
		switch n {
		case 1:
			w.WriteHeader(http.StatusNotFound)
		case 2:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"MultihashResults":[{"ProviderResults":[{"Provider":{"ID":"12D3KooOther","Addrs":[]}}]}]}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"MultihashResults":[{"ProviderResults":[{"Provider":{"ID":"`+id.String()+`","Addrs":[]}}]}]}`)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitIndexed(ctx, srv.Client(), srv.URL, root, id, every); err != nil {
		t.Fatalf("WaitIndexed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Fatalf("want 3 requests, got %d", count)
	}
	if path != "/cid/"+root.String() {
		t.Errorf("path %q", path)
	}
	if accept != "application/json" {
		t.Errorf("accept %q", accept)
	}
}

func TestWaitIndexedTimeout(t *testing.T) {
	id := testID(t)
	root := testCid(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := WaitIndexed(ctx, srv.Client(), srv.URL, root, id, every)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(err.Error(), "not indexed yet") {
		t.Errorf("err %q", err)
	}
}

func TestWaitIndexedBuildRequestError(t *testing.T) {
	id := testID(t)
	root := testCid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := WaitIndexed(ctx, http.DefaultClient, "http://exa mple.com", root, id, every)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "build request for") {
		t.Errorf("err %q, want it to mention building the request", err)
	}
	if strings.Contains(err.Error(), "<nil>") {
		t.Errorf("err %q, want it not to wrap a nil context error", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("err %q, want it not to be a context error", err)
	}
	if elapsed > time.Second {
		t.Errorf("WaitIndexed took %s, want it to fail at once instead of retrying", elapsed)
	}
}

func TestWaitIndexedInvalidJSON(t *testing.T) {
	id := testID(t)
	root := testCid(t)
	var mu sync.Mutex
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		if n == 1 {
			io.WriteString(w, "{not json")
			return
		}
		if n == 2 {
			io.WriteString(w, `{"MultihashResults":[]}`)
			return
		}
		io.WriteString(w, `{"MultihashResults":[{"ProviderResults":[{"Provider":{"ID":"`+id.String()+`","Addrs":[]}}]}]}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := WaitIndexed(ctx, srv.Client(), srv.URL, root, id, every); err != nil {
		t.Fatalf("WaitIndexed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Fatalf("want 3 requests, got %d", count)
	}
}
