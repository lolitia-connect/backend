package plugin

import (
	"database/sql"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	"github.com/perfect-panel/server/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestHostFunctionsEnforceCapabilitiesThroughWasmABI(t *testing.T) {
	ctx := t.Context()
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	opts := asynq.RedisClientOpt{Addr: mini.Addr()}
	queue := asynq.NewClient(opts)
	inspector := asynq.NewInspector(opts)
	t.Cleanup(func() { _ = inspector.Close(); _ = queue.Close() })
	database, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.ExecContext(ctx, `CREATE TABLE document (id INTEGER PRIMARY KEY, title TEXT, "show" INTEGER);
		INSERT INTO document VALUES (7, 'Getting started', 1), (8, 'Unpublished draft', 0)`)
	require.NoError(t, err)
	manager := NewManager(&HostEnv{
		Config: config.Config{Site: config.SiteConfig{SiteName: "Fixture"}},
		Store:  NewStoreAdapter(database, "sqlite"),
		Redis:  NewRedisAdapter(redisClient),
		Queue:  NewQueueAdapter(queue),
	})
	defer manager.Stop()

	const pluginName = "host-fixture"
	manifest := &PluginManifest{Permissions: []string{string(PermRedis), string(PermConfigRead), string(PermQueue), string(PermDatabaseRead)}}
	names := []string{"host_config_get", "host_redis_get", "host_async_submit", "host_async_wait_any", "host_async_resolve", "host_enqueue_task", "host_db_query"}
	manager.plugins[pluginName] = &PluginInstance{
		Name:        pluginName,
		Status:      StatusRunning,
		Manifest:    manifest,
		AsyncNotify: make(chan uint64, 1),
		AsyncSem:    make(chan struct{}, 1),
	}

	runtime := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = runtime.Close(ctx) })
	compiled, err := runtime.CompileModule(ctx, forwardingGuestWASM(names))
	if err != nil {
		t.Fatal(err)
	}
	hostFuncs := manager.buildHostFunctions(pluginName, manifest)
	pool, err := InstantiateWithHostFuncs(ctx, runtime, compiled, pluginName, hostFuncs, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(ctx) })
	module, err := pool.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Put(module)

	call := func(name string, request, reply proto.Message) {
		t.Helper()
		got, err := CallGuestFn(ctx, module, name, request, func() proto.Message { return reply })
		require.NoError(t, err, name)
		require.NotNil(t, got, name)
	}
	var site pluginv1.ConfigGetResponse
	call("host_config_get", &pluginv1.ConfigGetRequest{Key: "Site.SiteName"}, &site)
	require.True(t, site.Exists)
	require.Equal(t, "Fixture", site.Value)

	params, err := proto.Marshal(&pluginv1.RedisSetRequest{Key: "site", Value: site.Value, TtlSeconds: 30})
	require.NoError(t, err)
	var job pluginv1.AsyncSubmitResponse
	call("host_async_submit", &pluginv1.AsyncSubmitRequest{OpType: "redis_set", Params: params}, &job)
	require.NotZero(t, job.TaskId)
	var ready pluginv1.AsyncWaitAnyResponse
	call("host_async_wait_any", &pluginv1.AsyncWaitAnyRequest{}, &ready)
	require.Equal(t, job.TaskId, ready.TaskId)
	var completed pluginv1.AsyncResolveResponse
	call("host_async_resolve", &pluginv1.AsyncResolveRequest{TaskId: ready.TaskId}, &completed)
	require.True(t, completed.Done)
	require.Empty(t, completed.Error)
	var saved pluginv1.BoolResult
	require.NoError(t, proto.Unmarshal(completed.Result, &saved))
	require.True(t, saved.Success)
	var value pluginv1.RedisGetResponse
	call("host_redis_get", &pluginv1.RedisGetRequest{Key: "site"}, &value)
	require.True(t, value.Exists)
	require.Equal(t, "Fixture", value.Value)
	require.Equal(t, 30*time.Second, mini.TTL("plugin:"+pluginName+":site"))

	published, err := structpb.NewStruct(map[string]any{"show": 1})
	require.NoError(t, err)
	var refused pluginv1.DbQueryResponse
	call("host_db_query", &pluginv1.DbQueryRequest{Model: "document", Operation: "delete", Conditions: published}, &refused)
	require.Contains(t, refused.Error, "database_write")
	var catalog pluginv1.DbQueryResponse
	call("host_db_query", &pluginv1.DbQueryRequest{Model: "document", Operation: "list", Conditions: published,
		Fields: &fieldmaskpb.FieldMask{Paths: []string{"title"}}}, &catalog)
	require.Empty(t, catalog.Error)
	require.EqualValues(t, 1, catalog.Total)
	require.Len(t, catalog.Rows, 1)
	require.Equal(t, map[string]any{"title": "Getting started"}, catalog.Rows[0].AsMap())
	payload, err := structpb.NewStruct(map[string]any{"site": value.Value, "featured": catalog.Rows[0].AsMap()})
	require.NoError(t, err)
	var accepted pluginv1.BoolResult
	call("host_enqueue_task", &pluginv1.EnqueueTaskRequest{TaskName: "build-site-report", Payload: payload}, &accepted)
	require.True(t, accepted.Success, accepted.Error)
	jobs, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, "build-site-report", jobs[0].Type)
	require.JSONEq(t, `{"site":"Fixture","featured":{"title":"Getting started"}}`, string(jobs[0].Payload))
}

// The guest forwards each protobuf request and returns the host's actual pointer and length.
func forwardingGuestWASM(names []string) []byte {
	const i32, i64, abi, allocation = 0x7f, 0x7e, 0, 1
	count := byte(len(names))

	module := []byte("\x00asm\x01\x00\x00\x00")
	section := func(id byte, contents []byte) {
		module = append(module, id)
		module = appendWasmLengthAndBytes(module, contents)
	}
	section(1, []byte{2, 0x60, 2, i32, i32, 1, i64, 0x60, 1, i32, 1, i32})

	imports := []byte{count}
	for _, name := range names {
		imports = appendWasmLengthAndBytes(imports, []byte("env"))
		imports = appendWasmLengthAndBytes(imports, []byte(name))
		imports = append(imports, 0, abi)
	}
	section(2, imports)
	functions := []byte{count + 1, allocation}
	for range names {
		functions = append(functions, abi)
	}
	section(3, functions)
	section(5, []byte{1, 0, 1})

	exports := appendWasmLengthAndBytes([]byte{count + 2}, []byte("memory"))
	exports = append(exports, 2, 0)
	exports = appendWasmLengthAndBytes(exports, []byte("allocate"))
	exports = append(exports, 0, count)
	code := []byte{count + 1}
	allocate := appendWasmConst([]byte{0}, 0x41, guestScratch)
	code = appendWasmLengthAndBytes(code, append(allocate, 0x0b))
	for i, name := range names {
		exports = appendWasmLengthAndBytes(exports, []byte(name))
		exports = append(exports, 0, count+1+byte(i))
		// local.get ptr, local.get length, call import, return.
		code = appendWasmLengthAndBytes(code, []byte{0, 0x20, 0, 0x20, 1, 0x10, byte(i), 0x0b})
	}
	section(7, exports)
	section(10, code)
	return module
}
