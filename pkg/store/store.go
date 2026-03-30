// Package store provides an in-memory data store with optional JSON file persistence.
// It serves as the storage backend for all paperclip-go domain entities.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is a generic interface that all stored entities must satisfy.
type Record interface {
	GetID() string
	GetCompanyID() string
}

// Collection is a thread-safe, typed, in-memory collection of records.
type Collection[T Record] struct {
	mu      sync.RWMutex
	items   map[string]T
	ordKeys []string // insertion order
}

// NewCollection creates a new empty collection.
func NewCollection[T Record]() *Collection[T] {
	return &Collection[T]{
		items:   make(map[string]T),
		ordKeys: make([]string, 0),
	}
}

// Put inserts or updates a record.
func (c *Collection[T]) Put(item T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := item.GetID()
	if _, exists := c.items[id]; !exists {
		c.ordKeys = append(c.ordKeys, id)
	}
	c.items[id] = item
}

// Get retrieves a record by ID.
func (c *Collection[T]) Get(id string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.items[id]
	return item, ok
}

// Delete removes a record by ID.
func (c *Collection[T]) Delete(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[id]; !exists {
		return false
	}
	delete(c.items, id)
	for i, k := range c.ordKeys {
		if k == id {
			c.ordKeys = append(c.ordKeys[:i], c.ordKeys[i+1:]...)
			break
		}
	}
	return true
}

// List returns all records in insertion order.
func (c *Collection[T]) List() []T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]T, 0, len(c.ordKeys))
	for _, k := range c.ordKeys {
		if item, ok := c.items[k]; ok {
			result = append(result, item)
		}
	}
	return result
}

// Filter returns records matching a predicate.
func (c *Collection[T]) Filter(fn func(T) bool) []T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var result []T
	for _, k := range c.ordKeys {
		if item, ok := c.items[k]; ok && fn(item) {
			result = append(result, item)
		}
	}
	return result
}

// Count returns the number of records.
func (c *Collection[T]) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Store is the top-level in-memory store with JSON persistence.
type Store struct {
	mu       sync.RWMutex
	dataDir  string
	lastSave time.Time
}

// New creates a new Store. If dataDir is empty, persistence is disabled.
func New(dataDir string) *Store {
	return &Store{
		dataDir: dataDir,
	}
}

// SaveCollection persists a collection to a JSON file.
func SaveCollection[T Record](s *Store, name string, c *Collection[T]) error {
	if s.dataDir == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	items := c.List()
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}

	path := filepath.Join(s.dataDir, name+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	s.lastSave = time.Now()
	return nil
}

// LoadCollection reads a JSON file into a collection.
func LoadCollection[T Record](s *Store, name string, c *Collection[T]) error {
	if s.dataDir == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dataDir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("unmarshal %s: %w", name, err)
	}

	for _, item := range items {
		c.Put(item)
	}
	return nil
}

// DataDir returns the configured data directory.
func (s *Store) DataDir() string {
	return s.dataDir
}

// LastSave returns the time of the last save operation.
func (s *Store) LastSave() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSave
}
