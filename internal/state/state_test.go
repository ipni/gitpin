package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStateFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoadEmptyDir(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, want 1", s.Version)
	}
	if s.Roots == nil {
		t.Error("Roots is nil, want non-nil empty")
	}
	if len(s.Roots) != 0 {
		t.Errorf("len(Roots) = %d, want 0", len(s.Roots))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := State{
		Version: 1,
		PeerID:  "12D3KooWPeer",
		BaseURL: "https://x.github.io",
		Head:    "bafyhead",
		Roots: []Root{
			{Name: "a", CID: "bafya", Ad: "bafad1"},
			{Name: "b", CID: "bafyb", Ad: "bafad2"},
		},
	}
	if err := Save(dir, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Head != in.Head || out.PeerID != in.PeerID || out.BaseURL != in.BaseURL {
		t.Errorf("scalar fields mismatch: %+v vs %+v", out, in)
	}
	if len(out.Roots) != 2 || out.Roots[0].Name != "a" || out.Roots[1].Ad != "bafad2" {
		t.Errorf("Roots mismatch: %+v", out.Roots)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if data[len(data)-1] != '\n' {
		t.Error("file does not end in newline")
	}
	if !strings.Contains(string(data), "\n  \"version\": 1") {
		t.Errorf("expected two-space indent, got:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".tmp")); !os.IsNotExist(err) {
		t.Error("gitpin.json.tmp still present")
	}
}

func TestSaveNilRootsWritesEmptyArray(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, State{Version: 1, Roots: nil}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), `"roots": []`) {
		t.Errorf("want \"roots\": [], got:\n%s", data)
	}
}

func TestLoadUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	writeStateFile(t, dir, `{"version": 2, "roots": []}`)
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported version 2") {
		t.Fatalf("err = %v, want it to contain unsupported version 2", err)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeStateFile(t, dir, `{not json`)
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "gitpin.json") {
		t.Fatalf("err = %v, want it to name gitpin.json", err)
	}
}

func TestLoadNullRoots(t *testing.T) {
	dir := t.TempDir()
	writeStateFile(t, dir, `{"version": 1, "roots": null}`)
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Roots == nil {
		t.Error("Roots nil, want non-nil empty")
	}
	if len(s.Roots) != 0 {
		t.Errorf("len(Roots) = %d, want 0", len(s.Roots))
	}
}

func TestLoadMissingVersion(t *testing.T) {
	dir := t.TempDir()
	writeStateFile(t, dir, `{"roots": []}`)
	if _, err := Load(dir); err == nil {
		t.Fatal("want error for missing version, got nil")
	}
}

func TestSaveCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "nested")
	if err := Save(dir, State{Version: 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("state file not created: %v", err)
	}
}

func TestSaveOmitsEmptyHead(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, State{Version: 1, Head: ""}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), `"head"`) {
		t.Errorf("want head omitted, got:\n%s", data)
	}
}
