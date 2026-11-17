// Package state reads and writes the gitpin.json file that records the
// published state of a gitpin site.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName is the name of the state file inside the site directory.
const FileName = "gitpin.json"

// Version is the only state file format version this tool understands.
const Version = 1

// Root is a single published content root and its advertisement.
type Root struct {
	Name string `json:"name"`
	CID  string `json:"cid"`
	Ad   string `json:"ad"`
}

// State is the on-disk state of a gitpin site.
type State struct {
	Version int    `json:"version"`
	PeerID  string `json:"peer_id"`
	BaseURL string `json:"base_url"`
	Head    string `json:"head,omitempty"`
	Roots   []Root `json:"roots"`
}

// Load reads the state file from siteDir. A missing file yields an empty
// version-1 state; a nil Roots slice decodes to an empty, non-nil slice.
func Load(siteDir string) (State, error) {
	path := filepath.Join(siteDir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{Version: 1, Roots: []Root{}}, nil
		}
		return State{}, fmt.Errorf("%s: %w", FileName, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("%s: %w", FileName, err)
	}
	if s.Version != Version {
		return State{}, fmt.Errorf("%s: unsupported version %d", FileName, s.Version)
	}
	if s.Roots == nil {
		s.Roots = []Root{}
	}
	return s, nil
}

// Save writes s to the state file in siteDir, creating the directory if
// needed and replacing the file atomically via a temporary file.
func Save(siteDir string, s State) error {
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	if s.Roots == nil {
		s.Roots = []Root{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	data = append(data, '\n')
	tmp := filepath.Join(siteDir, FileName+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	if err := os.Rename(tmp, filepath.Join(siteDir, FileName)); err != nil {
		return fmt.Errorf("%s: %w", FileName, err)
	}
	return nil
}
