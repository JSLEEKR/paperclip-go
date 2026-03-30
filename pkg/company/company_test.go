package company

import (
	"strings"
	"testing"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestService() *Service {
	s := store.New("")
	return NewService(s)
}

func TestCreateCompany(t *testing.T) {
	svc := newTestService()
	c, err := svc.Create(Company{
		ID:   "co-1",
		Name: "Acme AI",
		Slug: "acme-ai",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Name != "Acme AI" {
		t.Errorf("name = %q, want %q", c.Name, "Acme AI")
	}
	if c.Status != StatusActive {
		t.Errorf("status = %q, want %q", c.Status, StatusActive)
	}
	if c.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestCreateCompanyValidation(t *testing.T) {
	svc := newTestService()

	tests := []struct {
		name    string
		company Company
		wantErr string
	}{
		{"missing id", Company{Name: "A", Slug: "a"}, "id is required"},
		{"missing name", Company{ID: "1", Slug: "a"}, "name is required"},
		{"missing slug", Company{ID: "1", Name: "A"}, "slug is required"},
		{"whitespace name", Company{ID: "1", Name: "  ", Slug: "a"}, "name is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(tt.company)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestDuplicateSlug(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Company{ID: "co-1", Name: "Acme", Slug: "acme"})
	_, err := svc.Create(Company{ID: "co-2", Name: "Other", Slug: "acme"})
	if err == nil {
		t.Fatal("expected duplicate slug error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want slug exists", err.Error())
	}
}

func TestGetCompany(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Company{ID: "co-1", Name: "Acme", Slug: "acme"})

	c, err := svc.Get("co-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if c.Name != "Acme" {
		t.Errorf("name = %q, want %q", c.Name, "Acme")
	}
}

func TestGetCompanyNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetBySlug(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Company{ID: "co-1", Name: "Acme", Slug: "acme"})

	c, err := svc.GetBySlug("acme")
	if err != nil {
		t.Fatalf("get by slug: %v", err)
	}
	if c.ID != "co-1" {
		t.Errorf("id = %q, want %q", c.ID, "co-1")
	}
}

func TestListCompanies(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Company{ID: "co-1", Name: "A", Slug: "a"})
	_, _ = svc.Create(Company{ID: "co-2", Name: "B", Slug: "b"})

	companies := svc.List()
	if len(companies) != 2 {
		t.Errorf("count = %d, want 2", len(companies))
	}
}

func TestUpdateCompany(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Company{ID: "co-1", Name: "Acme", Slug: "acme"})

	c, err := svc.Update("co-1", func(c *Company) error {
		c.Name = "Acme AI Corp"
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if c.Name != "Acme AI Corp" {
		t.Errorf("name = %q, want %q", c.Name, "Acme AI Corp")
	}
}

func TestUpdateCompanySlugChange(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Company{ID: "co-1", Name: "A", Slug: "a"})
	_, _ = svc.Create(Company{ID: "co-2", Name: "B", Slug: "b"})

	// Change slug to a conflicting value
	_, err := svc.Update("co-1", func(c *Company) error {
		c.Slug = "b"
		return nil
	})
	if err == nil {
		t.Fatal("expected slug conflict error")
	}

	// Change slug to a non-conflicting value
	c, err := svc.Update("co-1", func(c *Company) error {
		c.Slug = "c"
		return nil
	})
	if err != nil {
		t.Fatalf("update slug: %v", err)
	}
	if c.Slug != "c" {
		t.Errorf("slug = %q, want %q", c.Slug, "c")
	}
}

func TestDeleteCompany(t *testing.T) {
	svc := newTestService()
	_, _ = svc.Create(Company{ID: "co-1", Name: "Acme", Slug: "acme"})

	if err := svc.Delete("co-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if svc.Count() != 0 {
		t.Errorf("count = %d, want 0", svc.Count())
	}

	// Slug should be released
	_, err := svc.Create(Company{ID: "co-2", Name: "New", Slug: "acme"})
	if err != nil {
		t.Fatalf("create after delete: %v", err)
	}
}

func TestDeleteCompanyNotFound(t *testing.T) {
	svc := newTestService()
	err := svc.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCompanyDefaultConfig(t *testing.T) {
	svc := newTestService()
	c, _ := svc.Create(Company{ID: "co-1", Name: "A", Slug: "a"})
	if c.Config.MaxAgents != 50 {
		t.Errorf("max_agents = %d, want 50", c.Config.MaxAgents)
	}
	if c.Config.MaxConcurrentRuns != 10 {
		t.Errorf("max_concurrent_runs = %d, want 10", c.Config.MaxConcurrentRuns)
	}
}

func TestCompanyCount(t *testing.T) {
	svc := newTestService()
	if svc.Count() != 0 {
		t.Errorf("initial count = %d, want 0", svc.Count())
	}
	_, _ = svc.Create(Company{ID: "co-1", Name: "A", Slug: "a"})
	if svc.Count() != 1 {
		t.Errorf("count = %d, want 1", svc.Count())
	}
}
