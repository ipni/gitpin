package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

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
