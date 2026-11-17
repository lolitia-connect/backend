package plugin

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/model/ticket"
	_ "modernc.org/sqlite"
)

func TestModelWhitelistAllowed(t *testing.T) {
	allowed := []string{
		"ads",
		"announcement",
		"auth_method",
		"coupon",
		"document",
		"node",
		"order",
		"payment",
		"server",
		"subscribe",
		"subscribe_application",
		"subscribe_group",
		"system",
		"system_log",
		"task",
		"ticket",
		"ticket_follow",
		"traffic_log",
		"user",
		"user_auth_method",
		"user_device",
		"user_device_online_record",
		"user_subscribe",
		"user_withdrawal",
	}
	for _, m := range allowed {
		if _, ok := modelWhitelist[m]; !ok {
			t.Errorf("model %q should be in whitelist", m)
		}
	}
}

func TestModelWhitelistBlocked(t *testing.T) {
	blocked := []string{"admin", "config", "secret", "", "User", "ORDER", "ticket_reply"}
	for _, m := range blocked {
		if _, ok := modelWhitelist[m]; ok {
			t.Errorf("model %q should NOT be in whitelist", m)
		}
	}
}

func TestModelWhitelistTableMapping(t *testing.T) {
	tests := []struct {
		model string
		table string
	}{
		{"ads", "ads"},
		{"announcement", "announcement"},
		{"auth_method", "auth_method"},
		{"coupon", "coupon"},
		{"document", "document"},
		{"node", "nodes"},
		{"order", "order"},
		{"payment", "payment"},
		{"server", "servers"},
		{"subscribe", "subscribe"},
		{"subscribe_application", "subscribe_application"},
		{"subscribe_group", "subscribe_group"},
		{"system", "system"},
		{"system_log", "system_logs"},
		{"task", "task"},
		{"ticket", "ticket"},
		{"ticket_follow", "ticket_follow"},
		{"traffic_log", "traffic_log"},
		{"user", "user"},
		{"user_auth_method", "user_auth_methods"},
		{"user_device", "user_device"},
		{"user_device_online_record", "user_device_online_record"},
		{"user_subscribe", "user_subscribe"},
		{"user_withdrawal", "withdrawals"},
	}

	for _, tt := range tests {
		table, ok := modelWhitelist[tt.model]
		if !ok {
			t.Errorf("model %q not found", tt.model)
			continue
		}
		if table != tt.table {
			t.Errorf("model %q → table %q, want %q", tt.model, table, tt.table)
		}
	}
}

// Verify NewStoreAdapter doesn't panic with nil db (lazy init)
func TestNewStoreAdapterNil(t *testing.T) {
	adapter := NewStoreAdapter(nil, "mysql")
	if adapter == nil {
		t.Fatal("NewStoreAdapter returned nil")
	}
	// Query should return error, not panic
	_, _, err := adapter.Query("user", "list", nil, nil, 10, 0)
	if err == nil {
		t.Error("expected error for nil db query")
	}
}

func TestStoreAdapterBlockedModel(t *testing.T) {
	adapter := NewStoreAdapter(nil, "mysql")
	_, _, err := adapter.Query("admin", "list", nil, nil, 10, 0)
	if err == nil {
		t.Fatal("expected error for blocked model")
	}
}

func TestStoreAdapterUnknownOperation(t *testing.T) {
	adapter := NewStoreAdapter(nil, "mysql")
	_, _, err := adapter.Query("user", "execute_raw_sql", nil, nil, 10, 0)
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestValidateDBFields(t *testing.T) {
	if err := validateDBFields("user", []string{"id", "balance"}); err != nil {
		t.Fatalf("expected allowed fields, got %v", err)
	}
	if err := validateDBFields("ticket", []string{"id", "title", "description"}); err != nil {
		t.Fatalf("expected ticket fields to be allowed, got %v", err)
	}
	if err := validateDBFields("ticket_follow", []string{"ticket_id", "content"}); err != nil {
		t.Fatalf("expected ticket follow fields to be allowed, got %v", err)
	}
	if err := validateDBFields("user_subscribe", []string{"id", "status", "expire_time"}); err != nil {
		t.Fatalf("expected user subscription fields to be allowed, got %v", err)
	}
	if err := validateDBFields("payment", []string{"id", "platform", "enable"}); err != nil {
		t.Fatalf("expected payment public fields to be allowed, got %v", err)
	}
	if err := validateDBFields("auth_method", []string{"id", "method", "enabled"}); err != nil {
		t.Fatalf("expected auth method public fields to be allowed, got %v", err)
	}
	if err := validateDBFields("system", []string{"id", "category", "key", "type"}); err != nil {
		t.Fatalf("expected system metadata fields to be allowed, got %v", err)
	}
}

func TestValidateDBFieldsRejectsSensitiveFields(t *testing.T) {
	tests := []struct {
		model string
		field string
	}{
		{"user", "password"},
		{"user", "algo"},
		{"user", "salt"},
		{"user_subscribe", "token"},
		{"user_subscribe", "uuid"},
		{"order", "subscribe_token"},
		{"payment", "config"},
		{"payment", "token"},
		{"auth_method", "config"},
		{"system", "value"},
		{"system_log", "content"},
		{"server", "protocols"},
	}
	for _, tt := range tests {
		if err := validateDBFields(tt.model, []string{tt.field}); err == nil {
			t.Fatalf("expected %s.%s to be rejected", tt.model, tt.field)
		}
	}
}

func TestValidateDBConditionFields(t *testing.T) {
	if err := validateDBConditionFields("order", map[string]interface{}{"status": "paid"}); err != nil {
		t.Fatalf("expected allowed condition field, got %v", err)
	}
	if err := validateDBConditionFields("ticket_reply", map[string]interface{}{"ticket_id": int64(1), "content": "hello"}); err != nil {
		t.Fatalf("expected ticket reply fields to be allowed, got %v", err)
	}
	if err := validateDBConditionFields("order", map[string]interface{}{"raw_sql": "1=1"}); err == nil {
		t.Fatal("expected raw_sql condition field to be rejected")
	}
}

func TestStoreAdapterTicketReplyRequiresCreate(t *testing.T) {
	adapter := NewStoreAdapter(nil, "mysql")
	_, _, err := adapter.query("ticket_reply", "update", map[string]interface{}{"ticket_id": int64(1), "content": "hello"}, nil, 0, 0)
	if err == nil {
		t.Fatal("expected unsupported ticket_reply operation to fail")
	}
}

func TestStoreAdapterTicketReplySafetyAndRollback(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.Exec(`CREATE TABLE ticket (id INTEGER PRIMARY KEY, status INTEGER NOT NULL, updated_at TIMESTAMP); CREATE TABLE ticket_follow (id INTEGER PRIMARY KEY, ticket_id INTEGER, "from" TEXT, type INTEGER, content TEXT, created_at TIMESTAMP); INSERT INTO ticket (id, status) VALUES (1, 1), (2, 4)`); err != nil {
		t.Fatal(err)
	}
	adapter := NewStoreAdapter(database, "sqlite")
	for _, conditions := range []map[string]interface{}{
		{"ticket_id": int64(2), "content": "closed"},
		{"ticket_id": int64(1)},
		{"ticket_id": int64(1), "content": "bad type", "type": int64(257)},
		{"ticket_id": int64(1), "content": "data:image/svg+xml;base64,PHN2Zz4=", "type": int64(2)},
		{"ticket_id": int64(1), "content": strings.Repeat("x", ticket.MaxFollowLength+1)},
	} {
		if _, _, err := adapter.Query("ticket_reply", "create", conditions, nil, 0, 0); err == nil {
			t.Fatalf("invalid reply accepted: %v", conditions["ticket_id"])
		}
	}
	if _, _, err := adapter.Query("ticket_reply", "create", map[string]interface{}{"ticket_id": int64(1), "content": "answer"}, nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	var count, status int
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_follow").Scan(&count); err != nil || count != 1 {
		t.Fatalf("reply count = %d, %v", count, err)
	}
	if err := database.QueryRow("SELECT status FROM ticket WHERE id = 1").Scan(&status); err != nil || status != ticket.Waiting {
		t.Fatalf("ticket status = %d, %v", status, err)
	}
	if _, err := database.Exec("CREATE TRIGGER reject_ticket_status BEFORE UPDATE ON ticket BEGIN SELECT RAISE(ABORT, 'status write failed'); END"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.Query("ticket_reply", "create", map[string]interface{}{"ticket_id": int64(1), "content": "another answer"}, nil, 0, 0); err == nil {
		t.Fatal("status failure ignored")
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM ticket_follow").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed reply was not rolled back: %d, %v", count, err)
	}
}
