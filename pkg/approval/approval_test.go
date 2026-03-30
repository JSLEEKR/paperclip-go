package approval

import (
	"errors"
	"strings"
	"testing"

	"github.com/JSLEEKR/paperclip-go/pkg/store"
)

func newTestService() *Service {
	return NewService(store.New(""))
}

func TestSubmitRequest(t *testing.T) {
	svc := newTestService()
	r, err := svc.Submit(Request{
		ID:          "req-1",
		CompanyID:   "co-1",
		Kind:        KindHireAgent,
		Title:       "Hire Claude",
		RequestedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if r.Status != StatusPending {
		t.Errorf("status = %q, want pending", r.Status)
	}
}

func TestSubmitValidation(t *testing.T) {
	svc := newTestService()
	tests := []struct {
		name    string
		req     Request
		wantErr string
	}{
		{"no id", Request{CompanyID: "co-1", Kind: KindHireAgent, Title: "A", RequestedBy: "u"}, "id is required"},
		{"no company", Request{ID: "1", Kind: KindHireAgent, Title: "A", RequestedBy: "u"}, "company_id is required"},
		{"no kind", Request{ID: "1", CompanyID: "co-1", Title: "A", RequestedBy: "u"}, "kind is required"},
		{"no title", Request{ID: "1", CompanyID: "co-1", Kind: KindHireAgent, RequestedBy: "u"}, "title is required"},
		{"no requester", Request{ID: "1", CompanyID: "co-1", Kind: KindHireAgent, Title: "A"}, "requested_by is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Submit(tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestApproveRequest(t *testing.T) {
	svc := newTestService()
	callbackCalled := false
	svc.OnApprove(KindHireAgent, func(req Request) error {
		callbackCalled = true
		return nil
	})

	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire Claude", RequestedBy: "user-1",
	})

	r, err := svc.Approve("req-1", "admin-1", "Looks good")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if r.Status != StatusApproved {
		t.Errorf("status = %q", r.Status)
	}
	if r.DecidedBy != "admin-1" {
		t.Errorf("decided_by = %q", r.DecidedBy)
	}
	if r.DecisionNote != "Looks good" {
		t.Errorf("note = %q", r.DecisionNote)
	}
	if r.DecidedAt == nil {
		t.Error("expected decided_at")
	}
	if !callbackCalled {
		t.Error("approve callback not called")
	}
}

func TestRejectRequest(t *testing.T) {
	svc := newTestService()
	callbackCalled := false
	svc.OnReject(KindHireAgent, func(req Request) error {
		callbackCalled = true
		return nil
	})

	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire Agent", RequestedBy: "user-1",
	})

	r, err := svc.Reject("req-1", "admin-1", "Not needed")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if r.Status != StatusRejected {
		t.Errorf("status = %q", r.Status)
	}
	if !callbackCalled {
		t.Error("reject callback not called")
	}
}

func TestDoubleDecision(t *testing.T) {
	svc := newTestService()
	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire", RequestedBy: "user-1",
	})
	svc.Approve("req-1", "admin-1", "ok")

	_, err := svc.Reject("req-1", "admin-2", "no")
	if err == nil {
		t.Fatal("expected error on double decision")
	}
	if !strings.Contains(err.Error(), "already decided") {
		t.Errorf("error = %q", err)
	}
}

func TestRequestRevision(t *testing.T) {
	svc := newTestService()
	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire", RequestedBy: "user-1",
	})

	r, err := svc.RequestRevision("req-1", "admin-1", "Need more details")
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if r.Status != StatusRevisionRequested {
		t.Errorf("status = %q", r.Status)
	}
}

func TestResubmit(t *testing.T) {
	svc := newTestService()
	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire", RequestedBy: "user-1",
	})
	svc.RequestRevision("req-1", "admin-1", "details")

	r, err := svc.Resubmit("req-1")
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if r.Status != StatusPending {
		t.Errorf("status = %q, want pending", r.Status)
	}
	if r.DecidedBy != "" {
		t.Error("decided_by should be cleared")
	}
}

func TestResubmitNonRevision(t *testing.T) {
	svc := newTestService()
	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire", RequestedBy: "user-1",
	})

	_, err := svc.Resubmit("req-1")
	if err == nil {
		t.Fatal("expected error resubmitting pending request")
	}
}

func TestAddComment(t *testing.T) {
	svc := newTestService()
	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire", RequestedBy: "user-1",
	})

	c, err := svc.AddComment("req-1", "co-1", "admin-1", "Let me review")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if c.Body != "Let me review" {
		t.Errorf("body = %q", c.Body)
	}

	comments := svc.GetComments("req-1")
	if len(comments) != 1 {
		t.Errorf("comments = %d, want 1", len(comments))
	}
}

func TestCommentOnMissingRequest(t *testing.T) {
	svc := newTestService()
	_, err := svc.AddComment("nonexistent", "co-1", "admin-1", "test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListPending(t *testing.T) {
	svc := newTestService()
	svc.Submit(Request{ID: "r-1", CompanyID: "co-1", Kind: KindHireAgent, Title: "A", RequestedBy: "u"})
	svc.Submit(Request{ID: "r-2", CompanyID: "co-1", Kind: KindBudgetChange, Title: "B", RequestedBy: "u"})
	svc.Submit(Request{ID: "r-3", CompanyID: "co-1", Kind: KindHireAgent, Title: "C", RequestedBy: "u"})
	svc.Approve("r-1", "admin", "ok")

	pending := svc.ListPending("co-1")
	if len(pending) != 2 {
		t.Errorf("pending = %d, want 2", len(pending))
	}
}

func TestListByCompany(t *testing.T) {
	svc := newTestService()
	svc.Submit(Request{ID: "r-1", CompanyID: "co-1", Kind: KindHireAgent, Title: "A", RequestedBy: "u"})
	svc.Submit(Request{ID: "r-2", CompanyID: "co-2", Kind: KindHireAgent, Title: "B", RequestedBy: "u"})

	requests := svc.ListByCompany("co-1")
	if len(requests) != 1 {
		t.Errorf("count = %d, want 1", len(requests))
	}
}

func TestIsDecided(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusPending, false},
		{StatusApproved, true},
		{StatusRejected, true},
		{StatusRevisionRequested, false},
	}
	for _, tt := range tests {
		r := Request{Status: tt.status}
		if got := r.IsDecided(); got != tt.want {
			t.Errorf("IsDecided(%s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestApproveCallbackError(t *testing.T) {
	svc := newTestService()
	svc.OnApprove(KindHireAgent, func(req Request) error {
		return errors.New("agent creation failed")
	})

	svc.Submit(Request{
		ID: "req-1", CompanyID: "co-1", Kind: KindHireAgent,
		Title: "Hire", RequestedBy: "user-1",
	})

	_, err := svc.Approve("req-1", "admin-1", "ok")
	if err == nil {
		t.Fatal("expected callback error")
	}
	if !strings.Contains(err.Error(), "agent creation failed") {
		t.Errorf("error = %q", err)
	}
}

func TestCount(t *testing.T) {
	svc := newTestService()
	if svc.Count() != 0 {
		t.Errorf("initial count = %d", svc.Count())
	}
	svc.Submit(Request{ID: "r-1", CompanyID: "co-1", Kind: KindHireAgent, Title: "A", RequestedBy: "u"})
	if svc.Count() != 1 {
		t.Errorf("count = %d, want 1", svc.Count())
	}
}
