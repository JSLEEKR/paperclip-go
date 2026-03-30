package store

import (
	"os"
	"path/filepath"
	"testing"
)

// testRecord implements Record for testing.
type testRecord struct {
	ID        string `json:"id"`
	CompanyID string `json:"company_id"`
	Name      string `json:"name"`
}

func (r testRecord) GetID() string        { return r.ID }
func (r testRecord) GetCompanyID() string  { return r.CompanyID }

func TestCollectionPutAndGet(t *testing.T) {
	c := NewCollection[testRecord]()
	r := testRecord{ID: "1", CompanyID: "co-1", Name: "Alice"}
	c.Put(r)

	got, ok := c.Get("1")
	if !ok {
		t.Fatal("expected record to exist")
	}
	if got.Name != "Alice" {
		t.Errorf("got name %q, want %q", got.Name, "Alice")
	}
}

func TestCollectionGetMissing(t *testing.T) {
	c := NewCollection[testRecord]()
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected record not to exist")
	}
}

func TestCollectionUpdate(t *testing.T) {
	c := NewCollection[testRecord]()
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "Alice"})
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "Bob"})

	got, _ := c.Get("1")
	if got.Name != "Bob" {
		t.Errorf("got name %q, want %q", got.Name, "Bob")
	}
	if c.Count() != 1 {
		t.Errorf("got count %d, want 1", c.Count())
	}
}

func TestCollectionDelete(t *testing.T) {
	c := NewCollection[testRecord]()
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "Alice"})

	if !c.Delete("1") {
		t.Error("expected delete to return true")
	}
	if c.Delete("1") {
		t.Error("expected second delete to return false")
	}
	if c.Count() != 0 {
		t.Errorf("got count %d, want 0", c.Count())
	}
}

func TestCollectionList(t *testing.T) {
	c := NewCollection[testRecord]()
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "Alice"})
	c.Put(testRecord{ID: "2", CompanyID: "co-1", Name: "Bob"})
	c.Put(testRecord{ID: "3", CompanyID: "co-2", Name: "Charlie"})

	items := c.List()
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	// Verify insertion order
	if items[0].ID != "1" || items[1].ID != "2" || items[2].ID != "3" {
		t.Error("items not in insertion order")
	}
}

func TestCollectionFilter(t *testing.T) {
	c := NewCollection[testRecord]()
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "Alice"})
	c.Put(testRecord{ID: "2", CompanyID: "co-1", Name: "Bob"})
	c.Put(testRecord{ID: "3", CompanyID: "co-2", Name: "Charlie"})

	filtered := c.Filter(func(r testRecord) bool {
		return r.CompanyID == "co-1"
	})
	if len(filtered) != 2 {
		t.Errorf("got %d filtered items, want 2", len(filtered))
	}
}

func TestCollectionCount(t *testing.T) {
	c := NewCollection[testRecord]()
	if c.Count() != 0 {
		t.Errorf("empty collection count = %d, want 0", c.Count())
	}
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "A"})
	c.Put(testRecord{ID: "2", CompanyID: "co-1", Name: "B"})
	if c.Count() != 2 {
		t.Errorf("count = %d, want 2", c.Count())
	}
}

func TestStoreNew(t *testing.T) {
	s := New("")
	if s.DataDir() != "" {
		t.Errorf("expected empty data dir, got %q", s.DataDir())
	}
	if !s.LastSave().IsZero() {
		t.Error("expected zero last save time")
	}
}

func TestStoreSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	c := NewCollection[testRecord]()
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "Alice"})
	c.Put(testRecord{ID: "2", CompanyID: "co-1", Name: "Bob"})

	if err := SaveCollection(s, "test", c); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(dir, "test.json")); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	// Load into new collection
	c2 := NewCollection[testRecord]()
	if err := LoadCollection(s, "test", c2); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c2.Count() != 2 {
		t.Errorf("loaded count = %d, want 2", c2.Count())
	}
	got, _ := c2.Get("1")
	if got.Name != "Alice" {
		t.Errorf("loaded name = %q, want %q", got.Name, "Alice")
	}
}

func TestStoreLoadMissing(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	c := NewCollection[testRecord]()
	// Loading a non-existent file should not error
	if err := LoadCollection(s, "nonexistent", c); err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if c.Count() != 0 {
		t.Errorf("count = %d, want 0", c.Count())
	}
}

func TestStoreSaveNoDir(t *testing.T) {
	s := New("")
	c := NewCollection[testRecord]()
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "Alice"})
	// Save with no dir should be a no-op
	if err := SaveCollection(s, "test", c); err != nil {
		t.Fatalf("save no dir: %v", err)
	}
}

func TestCollectionDeletePreservesOrder(t *testing.T) {
	c := NewCollection[testRecord]()
	c.Put(testRecord{ID: "1", CompanyID: "co-1", Name: "A"})
	c.Put(testRecord{ID: "2", CompanyID: "co-1", Name: "B"})
	c.Put(testRecord{ID: "3", CompanyID: "co-1", Name: "C"})

	c.Delete("2")
	items := c.List()
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != "1" || items[1].ID != "3" {
		t.Error("order not preserved after delete")
	}
}
