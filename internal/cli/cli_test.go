package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipni/gitpin/internal/keys"
)

// genKey makes a fresh signing key available through the environment.
func genKey(t *testing.T) {
	t.Helper()
	_, b64, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("GITPIN_KEY", b64)
}

// decodeHead reads the head and changed fields out of a publish summary line.
func decodeHead(t *testing.T, stdout string) (bool, string) {
	t.Helper()
	var s struct {
		Changed bool   `json:"changed"`
		Head    string `json:"head"`
	}
	if err := json.Unmarshal([]byte(stdout), &s); err != nil {
		t.Fatalf("decode publish summary %q: %v", stdout, err)
	}
	return s.Changed, s.Head
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestNoArgs(t *testing.T) {
	code, _, errb := run(t)
	if code != 64 {
		t.Fatalf("code = %d, want 64", code)
	}
	for _, w := range []string{"keygen", "publish", "announce", "wait-indexed"} {
		if !strings.Contains(errb, w) {
			t.Errorf("usage missing %q; stderr:\n%s", w, errb)
		}
	}
}

func TestHelpAndDashH(t *testing.T) {
	for _, a := range []string{"help", "-h"} {
		if code, _, _ := run(t, a); code != 64 {
			t.Errorf("%s: code = %d, want 64", a, code)
		}
	}
}

func TestKeygenExtra(t *testing.T) {
	if code, _, _ := run(t, "keygen", "extra"); code != 64 {
		t.Fatalf("code = %d, want 64", code)
	}
}

func TestUnknownSubcommand(t *testing.T) {
	if code, _, _ := run(t, "nope"); code != 64 {
		t.Fatalf("code = %d, want 64", code)
	}
}

func TestPublishMissingContent(t *testing.T) {
	if code, _, _ := run(t, "publish", "--site", "x", "--base-url", "https://a.github.io"); code != 64 {
		t.Fatalf("code = %d, want 64", code)
	}
}

func TestPublishExtraArg(t *testing.T) {
	if code, _, _ := run(t, "publish", "--content", "a", "--site", "b", "--base-url", "c", "extra"); code != 64 {
		t.Fatalf("code = %d, want 64", code)
	}
}

func TestAnnounceNoFlags(t *testing.T) {
	if code, _, _ := run(t, "announce"); code != 64 {
		t.Fatalf("code = %d, want 64", code)
	}
}

func TestPublishNoKey(t *testing.T) {
	t.Setenv("GITPIN_KEY", "")
	code, _, errb := run(t, "publish", "--content", "a", "--site", "b", "--base-url", "c")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.HasPrefix(errb, "gitpin: ") {
		t.Errorf("stderr = %q, want prefix \"gitpin: \"", errb)
	}
}

func TestAnnounceNothingToAnnounce(t *testing.T) {
	dir := t.TempDir()
	code, _, errb := run(t, "announce", "--site", dir, "--base-url", "https://a.github.io", "--wait-served", "0")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errb, "nothing to announce") {
		t.Errorf("stderr = %q, want it to contain \"nothing to announce\"", errb)
	}
}

func TestPublishRerunAfterUnpinAll(t *testing.T) {
	genKey(t)
	content := t.TempDir()
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(content, "a.txt"), []byte("one file\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	base := "https://a.github.io"
	if code, _, errb := run(t, "publish", "--content", content, "--site", siteDir, "--base-url", base); code != 0 {
		t.Fatalf("first publish exit %d, stderr: %s", code, errb)
	}
	if err := os.Remove(filepath.Join(content, "a.txt")); err != nil {
		t.Fatalf("remove a.txt: %v", err)
	}
	code, stdout, errb := run(t, "publish", "--content", content, "--site", siteDir, "--base-url", base)
	if code != 0 {
		t.Fatalf("unpin-all publish exit %d, stderr: %s", code, errb)
	}
	_, head := decodeHead(t, stdout)
	if head == "" {
		t.Fatalf("head is empty after the unpin-all run")
	}
	code, stdout, errb = run(t, "publish", "--content", content, "--site", siteDir, "--base-url", base)
	if code != 0 {
		t.Fatalf("re-run after unpinning everything exit %d, stderr: %s", code, errb)
	}
	changed, again := decodeHead(t, stdout)
	if changed {
		t.Errorf("changed = true, want false")
	}
	if again != head {
		t.Errorf("head = %s, want unchanged %s", again, head)
	}
}

func TestPublishBaseURLTrailingSlash(t *testing.T) {
	genKey(t)
	content := t.TempDir()
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(content, "a.txt"), []byte("one file\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if code, _, errb := run(t, "publish", "--content", content, "--site", siteDir, "--base-url", "https://a.github.io"); code != 0 {
		t.Fatalf("first publish exit %d, stderr: %s", code, errb)
	}
	code, stdout, errb := run(t, "publish", "--content", content, "--site", siteDir, "--base-url", "https://a.github.io/")
	if code != 0 {
		t.Fatalf("publish with a trailing slash exit %d, stderr: %s", code, errb)
	}
	changed, _ := decodeHead(t, stdout)
	if changed {
		t.Errorf("changed = true, want false: a trailing slash must not name a different site")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := [][2]string{
		{"https://Alice.github.io", "https://alice.github.io"},
		{"https://alice.github.io", "https://alice.github.io"},
		{"https://Alice.github.io/", "https://alice.github.io"},
		{"https://ALICE.GitHub.IO", "https://alice.github.io"},
	}
	for _, c := range cases {
		if got := normalizeBaseURL(c[0]); got != c[1] {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestPublishBaseURLCase(t *testing.T) {
	genKey(t)
	content := t.TempDir()
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(content, "a.txt"), []byte("one file\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if code, _, errb := run(t, "publish", "--content", content, "--site", siteDir, "--base-url", "https://Alice.github.io"); code != 0 {
		t.Fatalf("first publish exit %d, stderr: %s", code, errb)
	}
	code, stdout, errb := run(t, "publish", "--content", content, "--site", siteDir, "--base-url", "https://alice.github.io")
	if code != 0 {
		t.Fatalf("publish with a lowercase host exit %d, stderr: %s", code, errb)
	}
	changed, _ := decodeHead(t, stdout)
	if changed {
		t.Errorf("changed = true, want false: host case must not name a different site")
	}
}

func TestWaitIndexedEmpty(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := run(t, "wait-indexed", "--site", dir)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}
