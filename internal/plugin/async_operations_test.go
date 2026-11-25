package plugin

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	_ "modernc.org/sqlite"
)

func TestAsyncPluginOperationsRespectPermissionsAndNamespace(t *testing.T) {
	ctx := t.Context()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `CREATE TABLE document (id INTEGER PRIMARY KEY, title TEXT, content TEXT); INSERT INTO document VALUES (1,'Read me','Full instructions')`); err != nil {
		t.Fatal(err)
	}
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	manager := NewManager(&HostEnv{Redis: NewRedisAdapter(client), Store: NewStoreAdapter(db, "sqlite")})
	for name, permissions := range map[string][]string{
		"reader": {string(PermRedis), string(PermDatabaseRead)},
		"other":  {string(PermRedis)},
	} {
		manager.plugins[name] = &PluginInstance{Name: name, Status: StatusRunning, Manifest: &PluginManifest{Permissions: permissions},
			AsyncNotify: make(chan uint64, 1), AsyncSem: make(chan struct{}, 1)}
	}
	run := func(name, op string, req proto.Message) ([]byte, string) {
		t.Helper()
		params, err := proto.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		id := manager.submitAsyncTask(name, op, params)
		if id == 0 {
			t.Fatalf("plugin could not submit %s", op)
		}
		select {
		case ready := <-manager.plugins[name].AsyncNotify:
			if ready != id {
				t.Fatalf("completion for task %d, want %d", ready, id)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("plugin task %d (%s) did not complete", id, op)
		}
		value, reason, done := manager.resolveAsyncTask(name, id)
		if !done {
			t.Fatalf("task %d could not be resolved", id)
		}
		return value, reason
	}

	data, reason := run("reader", "redis_set", &pluginv1.RedisSetRequest{Key: "state", Value: "published", TtlSeconds: 5})
	var saved pluginv1.BoolResult
	if err := proto.Unmarshal(data, &saved); err != nil || reason != "" || !saved.Success {
		t.Fatalf("set state: success=%v, reason=%q, err=%v", saved.Success, reason, err)
	}
	get := &pluginv1.RedisGetRequest{Key: "state"}
	data, reason = run("reader", "redis_get", get)
	var state pluginv1.RedisGetResponse
	if err := proto.Unmarshal(data, &state); err != nil || reason != "" || !state.Exists || state.Value != "published" {
		t.Fatalf("owner read state: exists=%v, value=%q, reason=%q, err=%v", state.Exists, state.Value, reason, err)
	}
	if data, reason := run("other", "redis_get", get); len(data) != 0 || !strings.Contains(reason, "key not found") {
		t.Fatal("another plugin could read the published state")
	}
	mini.FastForward(6 * time.Second)
	if data, reason := run("reader", "redis_get", get); len(data) != 0 || !strings.Contains(reason, "key not found") {
		t.Fatal("plugin state outlived its TTL")
	}

	query := &pluginv1.DbQueryRequest{Model: "document", Operation: "list", Fields: &fieldmaskpb.FieldMask{Paths: []string{"title"}}}
	data, reason = run("reader", "db_query", query)
	var result pluginv1.DbQueryResponse
	if err := proto.Unmarshal(data, &result); err != nil || reason != "" || result.Total != 1 || len(result.Rows) != 1 {
		t.Fatalf("read documents: total=%d, rows=%d, reason=%q, err=%v", result.Total, len(result.Rows), reason, err)
	}
	row := result.Rows[0].AsMap()
	if len(row) != 1 || row["title"] != "Read me" {
		t.Fatalf("document projection = %v", row)
	}
	if data, reason := run("other", "db_query", query); len(data) != 0 || !strings.Contains(reason, "database_read") {
		t.Fatal("plugin without a database grant read the document")
	}
}
