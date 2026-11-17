package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/ipni/gitpin/internal/dag"
	"github.com/ipni/gitpin/internal/state"
)

const (
	cidA    = "bafkreicvtlvnbate2v4v2oijoggn2bnl2skxf2cp4vkzb3xtdkekbd677u"
	cidB    = "bafkreig7pzyokaqvit2igs564zfj4n4j726ex2auodpwfhfnnxnqgmqklq"
	cidC    = "bafkreidlepanl425dmi7tnud6cykmfzvlxvrcj35sgxashjztrsvxb4ubu"
	cidOld  = "bafkreihotrnzew2kupxia2t3ph2msn4ryahrqbdoyuyqszhcnel7qd27de"
	cidRoot = "bafkreib54ddndfm6zzky5qbq6nzjfy4dvhev6sl6qi23rfybxekl5g6r7m"
)

func mustCID(t *testing.T, s string) cid.Cid {
	t.Helper()
	c, err := cid.Decode(s)
	if err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
	return c
}

func TestBaseAddrValid(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://alice.github.io", "/dns/alice.github.io/tcp/443/https"},
		{"https://alice.github.io/", "/dns/alice.github.io/tcp/443/https"},
		{"https://Alice.GitHub.io", "/dns/alice.github.io/tcp/443/https"},
		{"https://pins.example.com:8443", "/dns/pins.example.com/tcp/8443/https"},
		{"http://127.0.0.1:5000", "/ip4/127.0.0.1/tcp/5000/http"},
		{"http://localhost:5000", "/ip4/127.0.0.1/tcp/5000/http"},
		{"http://LOCALHOST:5000", "/ip4/127.0.0.1/tcp/5000/http"},
		{"https://a.com:", "/dns/a.com/tcp/443/https"},
	}
	for _, c := range cases {
		m, err := BaseAddr(c.in)
		if err != nil {
			t.Errorf("BaseAddr(%q): err = %v", c.in, err)
			continue
		}
		if m.String() != c.want {
			t.Errorf("BaseAddr(%q) = %s, want %s", c.in, m.String(), c.want)
		}
	}
}

func TestBaseAddrPathError(t *testing.T) {
	_, err := BaseAddr("https://alice.github.io/repo")
	if err == nil || !strings.Contains(err.Error(), "project pages under a path are not supported") {
		t.Fatalf("err = %v, want it to mention project pages under a path", err)
	}
}

func TestBaseAddrHTTPSOnlyError(t *testing.T) {
	cases := []string{
		"http://alice.github.io",
		"ftp://x",
		"alice.github.io",
		"https://",
		"https://a.github.io/?x=1",
		"https://a.github.io/#f",
		"https://u@a.github.io",
		"https://a.github.io/repo?x=1",
		"https://a.com:0",
		"https://a.com:99999",
	}
	for _, in := range cases {
		_, err := BaseAddr(in)
		if err == nil || !strings.Contains(err.Error(), "base URL must use https") {
			t.Errorf("BaseAddr(%q): err = %v, want it to contain base URL must use https", in, err)
		}
	}
}

func TestBaseAddrOtherErrors(t *testing.T) {
	cases := []string{
		"http://127.0.0.1",
		"https://[::1]",
		"http://127.0.0.1:5000/x",
	}
	for _, in := range cases {
		if _, err := BaseAddr(in); err == nil {
			t.Errorf("BaseAddr(%q): want error, got nil", in)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func ipfsNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "ipfs"))
	if err != nil {
		t.Fatalf("readdir ipfs: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	return got
}

func TestAssemblePrune(t *testing.T) {
	dir := t.TempDir()
	ipfs := filepath.Join(dir, "ipfs")
	writeFile(t, filepath.Join(ipfs, cidA), "a")
	writeFile(t, filepath.Join(ipfs, cidB), "b")
	writeFile(t, filepath.Join(ipfs, cidC), "c")
	writeFile(t, filepath.Join(ipfs, cidOld), "stale")
	writeFile(t, filepath.Join(dir, "CNAME"), "custom.example\n")
	writeFile(t, filepath.Join(dir, state.FileName), `{"version":1,"roots":[]}`)
	writeFile(t, filepath.Join(dir, "ipni", "v1", "ad", "x"), "ad-x")

	st := state.State{Version: 1, PeerID: "12D3KooWPeer", BaseURL: "https://alice.github.io"}
	roots := []dag.Root{{Name: "f", CID: mustCID(t, cidRoot), Blocks: []cid.Cid{mustCID(t, cidA), mustCID(t, cidB)}}}
	if err := Assemble(dir, st, roots); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	got := ipfsNames(t, dir)
	want := map[string]bool{cidA: true, cidB: true, ProbeCID: true}
	if len(got) != len(want) {
		t.Fatalf("ipfs dir = %v, want %v", got, want)
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("missing %s in ipfs dir %v", n, got)
		}
	}

	for _, p := range []string{filepath.Join(ipfs, ProbeCID), filepath.Join(dir, ".nojekyll")} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if len(data) != 0 {
			t.Errorf("%s not empty: %q", p, data)
		}
	}

	for _, c := range []struct{ path, want string }{
		{filepath.Join(dir, "CNAME"), "custom.example\n"},
		{filepath.Join(dir, state.FileName), `{"version":1,"roots":[]}`},
		{filepath.Join(dir, "ipni", "v1", "ad", "x"), "ad-x"},
	} {
		data, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("read %s: %v", c.path, err)
		}
		if string(data) != c.want {
			t.Errorf("%s changed: got %q want %q", c.path, data, c.want)
		}
	}
}

func TestAssembleSharedBlocks(t *testing.T) {
	dir := t.TempDir()
	ipfs := filepath.Join(dir, "ipfs")
	writeFile(t, filepath.Join(ipfs, cidA), "a")
	writeFile(t, filepath.Join(ipfs, cidB), "b")

	st := state.State{Version: 1, PeerID: "p"}
	roots := []dag.Root{
		{Name: "r1", CID: mustCID(t, cidRoot), Blocks: []cid.Cid{mustCID(t, cidA)}},
		{Name: "r2", CID: mustCID(t, cidRoot), Blocks: []cid.Cid{mustCID(t, cidA), mustCID(t, cidB)}},
	}
	if err := Assemble(dir, st, roots); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	got := ipfsNames(t, dir)
	for _, n := range []string{cidA, cidB, ProbeCID} {
		if !got[n] {
			t.Errorf("expected %s to survive, ipfs dir = %v", n, got)
		}
	}
}

func TestAssembleEmptyRoots(t *testing.T) {
	dir := t.TempDir()
	ipfs := filepath.Join(dir, "ipfs")
	writeFile(t, filepath.Join(ipfs, cidA), "a")
	if err := Assemble(dir, state.State{Version: 1, PeerID: "p"}, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	got := ipfsNames(t, dir)
	if len(got) != 1 || !got[ProbeCID] {
		t.Errorf("ipfs dir = %v, want only %s", got, ProbeCID)
	}
}

func TestAssembleSubdirSurvives(t *testing.T) {
	dir := t.TempDir()
	ipfs := filepath.Join(dir, "ipfs")
	writeFile(t, filepath.Join(ipfs, "sub", "inner"), "inner")
	if err := os.Symlink("inner", filepath.Join(ipfs, "sub", "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := Assemble(dir, state.State{Version: 1, PeerID: "p"}, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ipfs, "sub", "inner")); err != nil {
		t.Errorf("subdir file removed: %v", err)
	}
	if fi, err := os.Lstat(filepath.Join(ipfs, "sub", "link")); err != nil {
		t.Errorf("symlink removed: %v", err)
	} else if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink is no longer a symlink")
	}
}

func TestAssembleCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newsite")
	if err := Assemble(dir, state.State{Version: 1, PeerID: "p"}, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	for _, p := range []string{
		filepath.Join(dir, "ipfs", ProbeCID),
		filepath.Join(dir, ".nojekyll"),
		filepath.Join(dir, "index.html"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

func TestAssembleModes(t *testing.T) {
	dir := t.TempDir()
	if err := Assemble(dir, state.State{Version: 1, PeerID: "p"}, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	for _, p := range []string{
		filepath.Join(dir, ".nojekyll"),
		filepath.Join(dir, "ipfs", ProbeCID),
		filepath.Join(dir, "index.html"),
	} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if fi.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %o, want 644", p, fi.Mode().Perm())
		}
	}
}

func collectSite(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return files
}

func TestAssembleIdempotent(t *testing.T) {
	dir := t.TempDir()
	st := state.State{Version: 1, PeerID: "12D3KooWPeer", Roots: []state.Root{{Name: "f", CID: cidRoot, Ad: "ad1"}}}
	roots := []dag.Root{{Name: "f", CID: mustCID(t, cidRoot), Blocks: []cid.Cid{mustCID(t, cidA)}}}
	if err := Assemble(dir, st, roots); err != nil {
		t.Fatalf("Assemble 1: %v", err)
	}
	first := collectSite(t, dir)
	if err := Assemble(dir, st, roots); err != nil {
		t.Fatalf("Assemble 2: %v", err)
	}
	second := collectSite(t, dir)
	if len(first) != len(second) {
		t.Fatalf("file set changed between runs: %v vs %v", first, second)
	}
	for name, content1 := range first {
		if content2, ok := second[name]; !ok {
			t.Errorf("file %s missing after second run", name)
		} else if content1 != content2 {
			t.Errorf("file %s differs between runs: %q vs %q", name, content1, content2)
		}
	}
}

func TestIndexHTMLContent(t *testing.T) {
	dir := t.TempDir()
	st := state.State{
		Version: 1,
		PeerID:  "12D3KooWPeerID",
		Roots:   []state.Root{{Name: "hello.txt", CID: cidRoot, Ad: "ad1"}},
	}
	if err := Assemble(dir, st, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	page := readFileStr(t, filepath.Join(dir, "index.html"))
	for _, want := range []string{
		"<title>gitpin</title>",
		"12D3KooWPeerID",
		cidRoot,
		"https://inbrowser.link/ipfs/" + cidRoot,
		"Open in browser (inbrowser.link)",
		"https://ipfs.io/ipfs/" + cidRoot,
		"https://dweb.link/ipfs/" + cidRoot,
		"https://cid.contact/cid/" + cidRoot,
		"Published on IPFS",
		"Served by GitHub Pages. Indexed by cid.contact. Generated by gitpin.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(page, "<script>") {
		t.Error("page contains a <script> tag")
	}
}

func TestIndexHTMLEscaping(t *testing.T) {
	dir := t.TempDir()
	st := state.State{
		Version: 1,
		PeerID:  "p",
		Roots: []state.Root{
			{Name: "<script>alert(1)</script>", CID: cidRoot, Ad: "ad1"},
			{Name: `quote"and&amp`, CID: cidA, Ad: "ad2"},
		},
	}
	if err := Assemble(dir, st, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	page := readFileStr(t, filepath.Join(dir, "index.html"))
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Error("script name not escaped")
	}
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Error("raw script string present")
	}
	if strings.Contains(page, "<script>") {
		t.Error("page contains a <script> tag")
	}
	escapedName := `quote&#34;and&amp;amp`
	if !strings.Contains(page, escapedName) {
		t.Errorf("name not escaped exactly, want %q in page", escapedName)
	}
	if strings.Contains(page, `quote"and&amp`) {
		t.Error("raw name present in page")
	}
}

func TestIndexHTMLEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := Assemble(dir, state.State{Version: 1, PeerID: "p"}, nil); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	page := readFileStr(t, filepath.Join(dir, "index.html"))
	if !strings.Contains(page, "Nothing published yet") {
		t.Error("empty state missing 'Nothing published yet'")
	}
}

func readFileStr(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
