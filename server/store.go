package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a flow or version does not exist.
var ErrNotFound = errors.New("not found")

// VersionInfo describes one published snapshot of a flow. This mirrors Dify's
// notion of a published workflow version: the draft is edited freely, and
// "publish" freezes it as an immutable, numbered version you can trace back to.
type VersionInfo struct {
	Version     int       `json:"version"`
	PublishedAt time.Time `json:"publishedAt"`
	Note        string    `json:"note,omitempty"`
}

// FlowMeta is the lightweight record for a flow (no graph payload).
type FlowMeta struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Mode             string        `json:"mode"` // workflow | advanced-chat
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
	PublishedVersion int           `json:"publishedVersion"` // 0 = never published
	Versions         []VersionInfo `json:"versions"`
}

// Store persists flows on disk. Layout:
//
//	<root>/<flowID>/meta.json
//	<root>/<flowID>/draft.yaml
//	<root>/<flowID>/versions/1.yaml
//	<root>/<flowID>/versions/2.yaml
//
// Storing the DSL as YAML keeps the on-disk format byte-compatible with Dify's
// own export, so a file lifted from here imports straight into Dify.
type Store struct {
	root string
	mu   sync.Mutex
}

// NewStore opens (creating if needed) a file-backed store rooted at dir.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: dir}, nil
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) dir(id string) string      { return filepath.Join(s.root, id) }
func (s *Store) metaPath(id string) string { return filepath.Join(s.dir(id), "meta.json") }
func (s *Store) draftPath(id string) string {
	return filepath.Join(s.dir(id), "draft.yaml")
}
func (s *Store) versionPath(id string, v int) string {
	return filepath.Join(s.dir(id), "versions", fmt.Sprintf("%d.yaml", v))
}

func (s *Store) readMeta(id string) (*FlowMeta, error) {
	b, err := os.ReadFile(s.metaPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var m FlowMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) writeMeta(m *FlowMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(m.ID), b, 0o644)
}

// emptyDSL returns a minimal valid DSL for a brand-new flow: a single start
// node and nothing else, so the editor opens on a canvas that already parses.
func emptyDSL(name, mode string) []byte {
	startNode := fmt.Sprintf(`version: "0.3.1"
kind: app
app:
  name: %q
  mode: %s
  icon: "🤖"
  icon_background: "#FFEAD5"
  description: ""
workflow:
  graph:
    nodes:
      - id: "start"
        type: custom
        position: {x: 80, y: 200}
        data:
          type: start
          title: Start
          variables: []
    edges: []
    viewport: {x: 0, y: 0, zoom: 1}
  features: {}
  environment_variables: []
  conversation_variables: []
`, name, mode)
	return []byte(startNode)
}

// List returns all flow metas, newest first.
func (s *Store) List() ([]FlowMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var out []FlowMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.readMeta(e.Name())
		if err != nil {
			continue // skip unreadable dirs
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Create makes a new flow with an empty (single-start-node) draft.
func (s *Store) Create(name, mode string) (*FlowMeta, error) {
	if mode == "" {
		mode = "workflow"
	}
	if name == "" {
		name = "Untitled Flow"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	if err := os.MkdirAll(filepath.Join(s.dir(id), "versions"), 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	m := &FlowMeta{ID: id, Name: name, Mode: mode, CreatedAt: now, UpdatedAt: now}
	if err := os.WriteFile(s.draftPath(id), emptyDSL(name, mode), 0o644); err != nil {
		return nil, err
	}
	if err := s.writeMeta(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Meta returns a flow's metadata.
func (s *Store) Meta(id string) (*FlowMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readMeta(id)
}

// Draft returns the current draft YAML.
func (s *Store) Draft(id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.readMeta(id); err != nil {
		return nil, err
	}
	return os.ReadFile(s.draftPath(id))
}

// SaveDraft overwrites the draft YAML and bumps UpdatedAt. It also syncs the
// flow name/mode from the DSL's app block so the list view stays accurate.
func (s *Store) SaveDraft(id string, yml []byte, name, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta(id)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.draftPath(id), yml, 0o644); err != nil {
		return err
	}
	if name != "" {
		m.Name = name
	}
	if mode != "" {
		m.Mode = mode
	}
	m.UpdatedAt = time.Now().UTC()
	return s.writeMeta(m)
}

// Publish freezes the current draft as the next version number.
func (s *Store) Publish(id, note string) (*VersionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta(id)
	if err != nil {
		return nil, err
	}
	draft, err := os.ReadFile(s.draftPath(id))
	if err != nil {
		return nil, err
	}
	next := m.PublishedVersion + 1
	if err := os.WriteFile(s.versionPath(id, next), draft, 0o644); err != nil {
		return nil, err
	}
	vi := VersionInfo{Version: next, PublishedAt: time.Now().UTC(), Note: note}
	m.PublishedVersion = next
	m.Versions = append(m.Versions, vi)
	m.UpdatedAt = vi.PublishedAt
	if err := s.writeMeta(m); err != nil {
		return nil, err
	}
	return &vi, nil
}

// Version returns the YAML of a published version.
func (s *Store) Version(id string, v int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.readMeta(id); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(s.versionPath(id, v))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// Restore copies a published version back over the draft (rollback).
func (s *Store) Restore(id string, v int) error {
	b, err := s.Version(id, v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta(id)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.draftPath(id), b, 0o644); err != nil {
		return err
	}
	m.UpdatedAt = time.Now().UTC()
	return s.writeMeta(m)
}

// Delete removes a flow and all its versions.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.readMeta(id); err != nil {
		return err
	}
	return os.RemoveAll(s.dir(id))
}
