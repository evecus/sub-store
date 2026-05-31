// Package store provides a simple JSON file-backed persistent key-value store.
// It mirrors the $.read / $.write / $.persistCache semantics of the original
// JavaScript implementation.
package store

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Store is a file-backed in-memory JSON store.
type Store struct {
	mu       sync.RWMutex
	path     string
	data     map[string]json.RawMessage
}

// New opens (or creates) the JSON data file at path and loads it into memory.
func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	s := &Store{
		path: path,
		data: make(map[string]json.RawMessage),
	}

	if _, err := os.Stat(path); err == nil {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading data file: %w", err)
		}
		if err := json.Unmarshal(raw, &s.data); err != nil {
			log.Printf("[store] Warning: data file is corrupt, starting fresh: %v", err)
			s.data = make(map[string]json.RawMessage)
		}
	}
	return s, nil
}

// Read returns the JSON value for key.  Returns nil if key is absent.
func (s *Store) Read(key string) json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key]
}

// ReadInto decodes the JSON value for key into dst. dst must be a pointer.
// Returns false if key is absent.
func (s *Store) ReadInto(key string, dst interface{}) bool {
	raw := s.Read(key)
	if raw == nil {
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		log.Printf("[store] ReadInto(%q): %v", key, err)
		return false
	}
	return true
}

// Write encodes v as JSON and stores it under key, then persists.
func (s *Store) Write(key string, v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.data[key] = raw
	s.mu.Unlock()
	return s.persist()
}

// WriteRaw stores pre-encoded JSON under key, then persists.
func (s *Store) WriteRaw(key string, raw json.RawMessage) error {
	s.mu.Lock()
	s.data[key] = raw
	s.mu.Unlock()
	return s.persist()
}

// Delete removes a key, then persists.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()
	return s.persist()
}

// RawData returns the full underlying map (caller must not mutate).
func (s *Store) RawData() map[string]json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]json.RawMessage, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

// RestoreRaw replaces all data with the provided map and persists.
func (s *Store) RestoreRaw(data map[string]json.RawMessage) error {
	s.mu.Lock()
	s.data = data
	s.mu.Unlock()
	return s.persist()
}

func (s *Store) persist() error {
	s.mu.RLock()
	raw, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---- Key constants ----

const (
	KeySettings              = "settings"
	KeySubs                  = "subs"
	KeyCollections           = "collections"
	KeyFiles                 = "files"
	KeyModules               = "modules"
	KeyArtifacts             = "artifacts"
	KeyRules                 = "rules"
	KeyTokens                = "tokens"
	KeyArchives              = "archives"
	KeyResourceCache         = "#sub-store-cached-resource"
	KeyHeadersResourceCache  = "#sub-store-cached-headers-resource"
	KeyScriptResourceCache   = "#sub-store-cached-script-resource"
	KeyLogs                  = "#sub-store-logs"
	KeySchemaVersion         = "schemaVersion"
	GistBackupKey            = "Auto Generated Sub-Store Backup"
	GistBackupFileName       = "Sub-Store"
	ArtifactRepositoryKey    = "Sub-Store Artifacts Repository"
)

// ---- Migration ----

const currentSchemaVersion = 10

// Migrate runs any pending data-schema migrations.
func (s *Store) Migrate() {
	var version int
	s.ReadInto(KeySchemaVersion, &version)

	if version < 1 {
		s.migrateV1()
	}
	// Future migrations: if version < N { s.migrateVN() }

	_ = s.Write(KeySchemaVersion, currentSchemaVersion)
	log.Printf("[store] schema version: %d", currentSchemaVersion)
}

// migrateV1 ensures top-level array keys are initialized.
func (s *Store) migrateV1() {
	for _, key := range []string{KeySubs, KeyCollections, KeyFiles, KeyModules, KeyArtifacts, KeyRules, KeyTokens, KeyArchives} {
		raw := s.Read(key)
		if raw == nil {
			_ = s.Write(key, []interface{}{})
		}
	}
	if raw := s.Read(KeySettings); raw == nil {
		_ = s.Write(KeySettings, map[string]interface{}{})
	}
}
