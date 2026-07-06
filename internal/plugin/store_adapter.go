package plugin

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// modelWhitelist 定义插件可查询的数据库表白名单
var modelWhitelist = map[string]string{
	"ads":                       "ads",
	"announcement":              "announcement",
	"auth_method":               "auth_method",
	"coupon":                    "coupon",
	"document":                  "document",
	"node":                      "nodes",
	"order":                     "order",
	"payment":                   "payment",
	"server":                    "servers",
	"subscribe":                 "subscribe",
	"subscribe_application":     "subscribe_application",
	"subscribe_group":           "subscribe_group",
	"system":                    "system",
	"system_log":                "system_logs",
	"task":                      "task",
	"ticket":                    "ticket",
	"ticket_follow":             "ticket_follow",
	"traffic_log":               "traffic_log",
	"user":                      "user",
	"user_auth_method":          "user_auth_methods",
	"user_device":               "user_device",
	"user_device_online_record": "user_device_online_record",
	"user_subscribe":            "user_subscribe",
	"user_withdrawal":           "user_withdrawal",
}

var modelFieldWhitelist = map[string]map[string]bool{
	"ads": allowFields(
		"id", "title", "type", "content", "description", "target_url", "start_time",
		"end_time", "status", "created_at", "updated_at",
	),
	"announcement": allowFields(
		"id", "title", "content", "show", "pinned", "popup", "created_at", "updated_at",
	),
	"auth_method": allowFields(
		"id", "method", "enabled", "created_at", "updated_at",
	),
	"coupon": allowFields(
		"id", "name", "code", "count", "type", "discount", "start_time", "expire_time",
		"user_limit", "subscribe", "used_count", "enable", "created_at", "updated_at",
	),
	"document": allowFields(
		"id", "title", "content", "tags", "show", "created_at", "updated_at",
	),
	"node": allowFields(
		"id", "name", "tags", "port", "address", "server_id", "protocol", "enabled",
		"sort", "created_at", "updated_at",
	),
	"order": allowFields(
		"id", "parent_id", "user_id", "order_no", "type", "quantity", "price", "amount",
		"gift_amount", "discount", "coupon", "coupon_discount", "commission", "payment_id",
		"method", "fee_amount", "trade_no", "status", "subscribe_id", "is_new",
		"created_at", "updated_at",
	),
	"payment": allowFields(
		"id", "name", "platform", "icon", "domain", "description", "fee_mode",
		"fee_percent", "fee_amount", "sort", "enable",
	),
	"server": allowFields(
		"id", "name", "country", "city", "address", "sort", "last_reported_at",
		"created_at", "updated_at",
	),
	"subscribe": allowFields(
		"id", "name", "language", "description", "unit_price", "unit_time", "discount",
		"replacement", "inventory", "traffic", "speed_limit", "device_limit", "quota",
		"nodes", "node_tags", "show", "sell", "sort", "deduction_ratio",
		"allow_deduction", "reset_cycle", "renewal_reset", "show_original_price",
		"created_at", "updated_at",
	),
	"subscribe_application": allowFields(
		"id", "name", "icon", "description", "scheme", "user_agent", "is_default",
		"subscribe_template", "output_format", "download_link", "created_at", "updated_at",
	),
	"subscribe_group": allowFields(
		"id", "name", "description", "created_at", "updated_at",
	),
	"system": allowFields(
		"id", "category", "key", "type", "desc", "created_at", "updated_at",
	),
	"system_log": allowFields(
		"id", "type", "date", "object_id", "created_at",
	),
	"task": allowFields(
		"id", "type", "scope", "content", "status", "errors", "total", "current",
		"created_at", "updated_at",
	),
	"ticket": allowFields(
		"id", "user_id", "status", "title", "description", "created_at", "updated_at",
	),
	"ticket_follow": allowFields(
		"id", "ticket_id", "from", "type", "content", "created_at",
	),
	"ticket_reply": allowFields(
		"ticket_id", "from", "type", "content",
	),
	"traffic_log": allowFields(
		"id", "server_id", "user_id", "subscribe_id", "download", "upload", "timestamp",
	),
	"user": allowFields(
		"id", "avatar", "balance", "refer_code", "referer_id", "commission",
		"referral_percentage", "only_first_purchase", "gift_amount", "enable", "is_admin",
		"enable_balance_notify", "enable_login_notify", "enable_subscribe_notify",
		"enable_trade_notify", "rules", "created_at", "updated_at",
	),
	"user_auth_method": allowFields(
		"id", "user_id", "auth_type", "auth_identifier", "verified", "created_at", "updated_at",
	),
	"user_device": allowFields(
		"id", "ip", "user_id", "user_agent", "identifier", "online", "enabled",
		"created_at", "updated_at",
	),
	"user_device_online_record": allowFields(
		"id", "user_id", "identifier", "online_time", "offline_time", "online_seconds",
		"duration_days", "created_at",
	),
	"user_subscribe": allowFields(
		"id", "user_id", "order_id", "subscribe_id", "start_time", "expire_time",
		"finished_at", "traffic", "download", "upload", "status", "note",
		"created_at", "updated_at",
	),
	"user_withdrawal": allowFields(
		"id", "user_id", "amount", "content", "status", "reason", "created_at", "updated_at",
	),
}

func allowFields(fields ...string) map[string]bool {
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	return allowed
}

// StoreAdapter 将 database/sql 适配为 StoreClient 接口
type StoreAdapter struct {
	db      *sql.DB
	dialect string
}

// NewStoreAdapter 创建数据库 Store 适配器
func NewStoreAdapter(db *sql.DB, dialect string) *StoreAdapter {
	return &StoreAdapter{db: db, dialect: dialect}
}

// Query 执行数据库查询（安全：仅白名单表 + 白名单操作）
func (a *StoreAdapter) Query(
	model string,
	operation string,
	conditions map[string]interface{},
	fields []string,
	limit, offset int32,
) ([]map[string]interface{}, int64, error) {
	if a.db == nil {
		return nil, 0, fmt.Errorf("database not available")
	}
	return a.query(model, operation, conditions, fields, limit, offset)
}

func (a *StoreAdapter) query(
	model string,
	operation string,
	conditions map[string]interface{},
	fields []string,
	limit, offset int32,
) ([]map[string]interface{}, int64, error) {
	if model == "ticket_reply" {
		return a.createTicketReply(operation, conditions)
	}

	table, ok := modelWhitelist[model]
	if !ok {
		return nil, 0, fmt.Errorf("model %q not allowed", model)
	}
	if err := validateDBFields(model, fields); err != nil {
		return nil, 0, err
	}
	if err := validateDBConditionFields(model, conditions); err != nil {
		return nil, 0, err
	}

	where, args := a.whereClause(conditions, 1)

	// 统计总数
	var total int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", quoteIdentifier(table), where)
	if err := a.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count %s: %w", model, err)
	}

	selectFields := "*"
	if len(fields) > 0 {
		quoted := make([]string, 0, len(fields))
		for _, field := range fields {
			quoted = append(quoted, quoteIdentifier(field))
		}
		selectFields = strings.Join(quoted, ", ")
	}

	switch operation {
	case "list", "find":
		querySQL := fmt.Sprintf("SELECT %s FROM %s%s", selectFields, quoteIdentifier(table), where)
		queryArgs := append([]interface{}{}, args...)
		if limit > 0 {
			querySQL += fmt.Sprintf(" LIMIT %s", a.placeholder(len(queryArgs)+1))
			queryArgs = append(queryArgs, int(limit))
		}
		if offset > 0 {
			querySQL += fmt.Sprintf(" OFFSET %s", a.placeholder(len(queryArgs)+1))
			queryArgs = append(queryArgs, int(offset))
		}
		rows, err := a.queryRows(querySQL, queryArgs...)
		if err != nil {
			return nil, 0, fmt.Errorf("query %s: %w", model, err)
		}
		return rows, total, nil

	case "create":
		if len(conditions) > 0 {
			columns, placeholders, insertArgs := a.insertParts(conditions)
			result, err := a.db.Exec(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteIdentifier(table), columns, placeholders), insertArgs...)
			if err != nil {
				return nil, 0, fmt.Errorf("create %s: %w", model, err)
			}
			affected, _ := result.RowsAffected()
			return []map[string]interface{}{{"affected": affected}}, affected, nil
		}
		return nil, 0, fmt.Errorf("create requires conditions")

	case "update":
		if len(conditions) > 0 {
			setClause, updateArgs := a.setClause(conditions)
			result, err := a.db.Exec(fmt.Sprintf("UPDATE %s SET %s%s", quoteIdentifier(table), setClause, where), append(updateArgs, args...)...)
			if err != nil {
				return nil, 0, fmt.Errorf("update %s: %w", model, err)
			}
			affected, _ := result.RowsAffected()
			return []map[string]interface{}{{"affected": affected}}, affected, nil
		}
		return nil, 0, fmt.Errorf("update requires conditions")

	case "delete":
		if len(conditions) > 0 {
			result, err := a.db.Exec(fmt.Sprintf("DELETE FROM %s%s", quoteIdentifier(table), where), args...)
			if err != nil {
				return nil, 0, fmt.Errorf("delete %s: %w", model, err)
			}
			affected, _ := result.RowsAffected()
			return []map[string]interface{}{{"affected": affected}}, affected, nil
		}
		return nil, 0, fmt.Errorf("delete requires conditions (use specific conditions to avoid mass deletion)")

	default:
		return nil, 0, fmt.Errorf("unknown operation %q", operation)
	}
}

func (a *StoreAdapter) createTicketReply(operation string, conditions map[string]interface{}) ([]map[string]interface{}, int64, error) {
	if operation != "create" {
		return nil, 0, fmt.Errorf("unknown operation %q for ticket_reply", operation)
	}
	if err := validateDBConditionFields("ticket_reply", conditions); err != nil {
		return nil, 0, err
	}

	ticketID, ok := int64FromValue(conditions["ticket_id"])
	if !ok || ticketID <= 0 {
		return nil, 0, fmt.Errorf("ticket_reply requires ticket_id")
	}
	content := fmt.Sprint(conditions["content"])
	if content == "" {
		return nil, 0, fmt.Errorf("ticket_reply requires content")
	}
	from := fmt.Sprint(conditions["from"])
	if from == "" {
		from = "admin"
	}
	replyType, ok := int64FromValue(conditions["type"])
	if !ok || replyType <= 0 {
		replyType = 1
	}

	tx, err := a.db.Begin()
	if err != nil {
		return nil, 0, err
	}
	err = func() error {
		var count int64
		if err := tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = %s", quoteIdentifier("ticket"), quoteIdentifier("id"), a.placeholder(1)), ticketID).Scan(&count); err != nil {
			return fmt.Errorf("find ticket: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("ticket %d not found", ticketID)
		}

		_, err := tx.Exec(fmt.Sprintf("INSERT INTO %s (%s, %s, %s, %s, %s) VALUES (%s, %s, %s, %s, %s)",
			quoteIdentifier("ticket_follow"), quoteIdentifier("ticket_id"), quoteIdentifier("from"), quoteIdentifier("type"), quoteIdentifier("content"), quoteIdentifier("created_at"),
			a.placeholder(1), a.placeholder(2), a.placeholder(3), a.placeholder(4), a.placeholder(5)),
			ticketID, from, uint8(replyType), content, time.Now())
		if err != nil {
			return fmt.Errorf("create ticket follow: %w", err)
		}

		_, err = tx.Exec(fmt.Sprintf("UPDATE %s SET %s = %s, %s = %s WHERE %s = %s",
			quoteIdentifier("ticket"), quoteIdentifier("status"), a.placeholder(1), quoteIdentifier("updated_at"), a.placeholder(2), quoteIdentifier("id"), a.placeholder(3)),
			2, time.Now(), ticketID)
		if err != nil {
			return fmt.Errorf("update ticket status: %w", err)
		}
		return nil
	}()
	if err != nil {
		_ = tx.Rollback()
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}

	return []map[string]interface{}{{"affected": int64(1)}}, 1, nil
}

func validateDBFields(model string, fields []string) error {
	allowed := modelFieldWhitelist[model]
	if len(allowed) == 0 {
		return fmt.Errorf("model %q has no field whitelist", model)
	}
	for _, field := range fields {
		if !allowed[field] {
			return fmt.Errorf("field %q not allowed for model %q", field, model)
		}
	}
	return nil
}

func int64FromValue(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case uint64:
		if v > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(v), true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	default:
		return 0, false
	}
}

func (a *StoreAdapter) placeholder(index int) string {
	if a.dialect == "postgres" || a.dialect == "pgx" {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}

func (a *StoreAdapter) whereClause(conditions map[string]interface{}, start int) (string, []interface{}) {
	if len(conditions) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(conditions))
	args := make([]interface{}, 0, len(conditions))
	idx := start
	for field, value := range conditions {
		parts = append(parts, fmt.Sprintf("%s = %s", quoteIdentifier(field), a.placeholder(idx)))
		args = append(args, value)
		idx++
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func (a *StoreAdapter) insertParts(values map[string]interface{}) (string, string, []interface{}) {
	columns := make([]string, 0, len(values))
	placeholders := make([]string, 0, len(values))
	args := make([]interface{}, 0, len(values))
	idx := 1
	for field, value := range values {
		columns = append(columns, quoteIdentifier(field))
		placeholders = append(placeholders, a.placeholder(idx))
		args = append(args, value)
		idx++
	}
	return strings.Join(columns, ", "), strings.Join(placeholders, ", "), args
}

func (a *StoreAdapter) setClause(values map[string]interface{}) (string, []interface{}) {
	parts := make([]string, 0, len(values))
	args := make([]interface{}, 0, len(values))
	idx := 1
	for field, value := range values {
		parts = append(parts, fmt.Sprintf("%s = %s", quoteIdentifier(field), a.placeholder(idx)))
		args = append(args, value)
		idx++
	}
	return strings.Join(parts, ", "), args
}

func (a *StoreAdapter) queryRows(query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			if b, ok := values[i].([]byte); ok {
				row[column] = string(b)
			} else {
				row[column] = values[i]
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func quoteIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func validateDBConditionFields(model string, conditions map[string]interface{}) error {
	allowed := modelFieldWhitelist[model]
	if len(allowed) == 0 {
		return fmt.Errorf("model %q has no field whitelist", model)
	}
	for field := range conditions {
		if !allowed[field] {
			return fmt.Errorf("condition field %q not allowed for model %q", field, model)
		}
	}
	return nil
}
