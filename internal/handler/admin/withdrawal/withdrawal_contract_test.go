package withdrawal

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
)

func newJSONContext(t *testing.T, body string) *hertzx.Context {
	t.Helper()
	raw := app.NewContext(0)
	raw.Request.SetRequestURI("/v1/admin/withdrawal/status")
	raw.Request.Header.SetMethod("PUT")
	raw.Request.Header.Set("Content-Type", "application/json")
	raw.Request.SetBodyString(body)
	return hertzx.NewContext(context.Background(), raw)
}

// The admin console sends the withdrawal id as a JSON *string* because the
// generated client types it as `string` (the Go type is int64 with a
// `json:"id,string"` tag). Binding must therefore accept "12" and reject a bare
// number, otherwise every review request either fails or silently reviews id 0.
func TestReviewWithdrawalBindsStringId(t *testing.T) {
	var req types.ReviewWithdrawalRequest
	if err := newJSONContext(t, `{"id":"12","status":2,"reason":"no"}`).ShouldBindJSON(&req); err != nil {
		t.Fatalf("bind string id: %v", err)
	}
	if req.Id != 12 || req.Status != 2 || req.Reason != "no" {
		t.Fatalf("bound = %+v, want id=12 status=2 reason=no", req)
	}
}

func TestReviewWithdrawalRejectsNumericId(t *testing.T) {
	var req types.ReviewWithdrawalRequest
	if err := newJSONContext(t, `{"id":12,"status":1}`).ShouldBindJSON(&req); err == nil {
		t.Fatalf("a bare numeric id must not bind to a ,string field, got %+v", req)
	}
}

// Approval carries no reason; the reviewer's console omits the field entirely
// for an approval.
func TestReviewWithdrawalBindsApprovalWithoutReason(t *testing.T) {
	var req types.ReviewWithdrawalRequest
	if err := newJSONContext(t, `{"id":"7","status":1}`).ShouldBindJSON(&req); err != nil {
		t.Fatalf("bind approval: %v", err)
	}
	if req.Id != 7 || req.Status != 1 || req.Reason != "" {
		t.Fatalf("bound = %+v, want id=7 status=1 reason empty", req)
	}
}

// The list filters arrive as query parameters, including a status of 0 which is
// falsy in most languages and must not be dropped.
func TestGetWithdrawalListBindsQueryFilters(t *testing.T) {
	raw := app.NewContext(0)
	raw.Request.SetRequestURI("/v1/admin/withdrawal/list?page=2&size=50&status=0&user_id=9")
	ctx := hertzx.NewContext(context.Background(), raw)

	var req types.GetWithdrawalListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		t.Fatalf("bind query: %v", err)
	}
	if req.Page != 2 || req.Size != 50 {
		t.Fatalf("page/size = %d/%d, want 2/50", req.Page, req.Size)
	}
	if req.Status == nil || *req.Status != 0 {
		t.Fatalf("status = %v, want a pointer to 0", req.Status)
	}
	if req.UserId == nil || *req.UserId != 9 {
		t.Fatalf("user_id = %v, want a pointer to 9", req.UserId)
	}
}

// An omitted status must stay nil so the query is not filtered, rather than
// defaulting to "pending".
func TestGetWithdrawalListLeavesOmittedFiltersNil(t *testing.T) {
	raw := app.NewContext(0)
	raw.Request.SetRequestURI("/v1/admin/withdrawal/list?page=1&size=20")
	ctx := hertzx.NewContext(context.Background(), raw)

	var req types.GetWithdrawalListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		t.Fatalf("bind query: %v", err)
	}
	if req.Status != nil {
		t.Fatalf("status = %v, want nil", *req.Status)
	}
	if req.UserId != nil {
		t.Fatalf("user_id = %v, want nil", *req.UserId)
	}
}

// The response the console consumes: ids as strings, amounts in minor units and
// timestamps in milliseconds.
func TestWithdrawalLogResponseShape(t *testing.T) {
	body, err := json.Marshal(types.WithdrawalLog{
		Id: 5, UserId: 9, Amount: 1200, Content: "bank", Status: 2, Reason: "no",
		CreatedAt: 1700000000000, UpdatedAt: 1700000001000,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for field, want := range map[string]any{"id": "5", "user_id": "9"} {
		if got := decoded[field]; got != want {
			t.Errorf("%s = %#v, want %q", field, got, want)
		}
	}
	for field, want := range map[string]float64{"amount": 1200, "created_at": 1700000000000, "updated_at": 1700000001000} {
		if got := decoded[field]; got != want {
			t.Errorf("%s = %#v, want %v", field, got, want)
		}
	}
}
