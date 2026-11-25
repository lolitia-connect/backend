package plugin

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pluginv1 "github.com/perfect-panel/server/api/plugin/v1"
	"github.com/perfect-panel/server/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero/api"
	"google.golang.org/protobuf/proto"
)

const (
	lifecycleGuestName = "lifecycle-guest"
	guestScratch       = 4096
)

func TestManagerGuestLifecycleInstallEnableDisableReplace(t *testing.T) {
	mgr, recorder := newLifecycleGuestManager(t)
	v1 := lifecycleGuest{version: "v1"}
	v2 := lifecycleGuest{version: "v2"}

	installed, err := mgr.InstallPluginArchive(t.Context(), v1.archive(t), PluginInstallOptions{})
	require.NoError(t, err)
	require.False(t, installed.Enabled)
	require.False(t, installed.Replaced)
	require.Equal(t, StatusUnloaded, installed.Status)
	require.True(t, installed.Validation.Valid, "%+v", installed.Validation)
	require.Equal(t, v1.version, installed.Validation.Manifest.Version)
	require.FileExists(t, filepath.Join(mgr.PluginDirectory(), lifecycleGuestName, "bin", "guest.wasm"))
	require.Nil(t, mgr.GetPlugin(lifecycleGuestName))
	require.Empty(t, recorder.writes)
	assertLifecycleGuestInactive(t, mgr, StatusUnloaded)

	require.NoError(t, mgr.EnablePlugin(lifecycleGuestName))
	first := assertLifecycleGuestRunning(t, mgr, v1)
	require.NoError(t, mgr.EnablePlugin(lifecycleGuestName))
	assertLifecycleWrites(t, recorder, "init:v1", "start:v1")

	require.NoError(t, mgr.DisablePlugin(lifecycleGuestName))
	require.True(t, first.IsClosed())
	require.Nil(t, mgr.GetPlugin(lifecycleGuestName))
	assertLifecycleGuestInactive(t, mgr, StatusUnloaded)
	info, found := mgr.GetInstalledPluginInfo(lifecycleGuestName)
	require.True(t, found)
	require.Equal(t, StatusUnloaded, info.Status)
	require.Equal(t, v1.version, info.Version)
	assertLifecycleWrites(t, recorder, "init:v1", "start:v1", "stop:v1")

	require.NoError(t, mgr.EnablePlugin(lifecycleGuestName))
	second := assertLifecycleGuestRunning(t, mgr, v1)
	require.NotSame(t, first, second)

	_, err = mgr.InstallPluginArchive(t.Context(), v2.archive(t), PluginInstallOptions{Enable: true})
	require.ErrorContains(t, err, "already exists")
	require.Same(t, second, assertLifecycleGuestRunning(t, mgr, v1))
	assertLifecycleWrites(t, recorder, "init:v1", "start:v1", "stop:v1", "init:v1", "start:v1")

	replaced, err := mgr.InstallPluginArchive(t.Context(), v2.archive(t), PluginInstallOptions{Replace: true, Enable: true})
	require.NoError(t, err)
	require.True(t, replaced.Replaced)
	require.True(t, replaced.Enabled)
	require.Equal(t, StatusRunning, replaced.Status)
	require.True(t, replaced.Validation.Valid, "%+v", replaced.Validation)
	require.True(t, second.IsClosed())
	assertLifecycleGuestRunning(t, mgr, v2)
	assertLifecycleWrites(t, recorder,
		"init:v1", "start:v1", "stop:v1", "init:v1", "start:v1", "stop:v1", "init:v2", "start:v2")
}

func TestManagerGuestLifecycleReloadFromDisk(t *testing.T) {
	mgr, recorder := newLifecycleGuestManager(t)
	v1 := lifecycleGuest{version: "v1"}
	v2 := lifecycleGuest{version: "v2"}
	v3 := lifecycleGuest{version: "v3"}

	_, err := mgr.InstallPluginArchive(t.Context(), v1.archive(t), PluginInstallOptions{Enable: true})
	require.NoError(t, err)
	first := assertLifecycleGuestRunning(t, mgr, v1)

	wasmPath := filepath.Join(mgr.PluginDirectory(), lifecycleGuestName, "bin", "guest.wasm")
	require.NoError(t, os.WriteFile(wasmPath, []byte("invalid WASM"), 0600))
	invalid := mgr.ValidateInstalledPlugin(lifecycleGuestName)
	require.False(t, invalid.Valid)
	require.Contains(t, invalid.Error, "compile wasm module")
	require.Same(t, first, assertLifecycleGuestRunning(t, mgr, v1))

	v2.writeInstalled(t, mgr.PluginDirectory())
	validation := mgr.ValidateInstalledPlugin(lifecycleGuestName)
	require.True(t, validation.Valid, "%+v", validation)
	require.Equal(t, v2.version, validation.Manifest.Version)
	require.Same(t, first, assertLifecycleGuestRunning(t, mgr, v1))
	assertLifecycleWrites(t, recorder, "init:v1", "start:v1")

	require.NoError(t, mgr.ReloadPlugin(lifecycleGuestName))
	require.True(t, first.IsClosed())
	second := assertLifecycleGuestRunning(t, mgr, v2)
	assertLifecycleWrites(t, recorder, "init:v1", "start:v1", "stop:v1", "init:v2", "start:v2")

	v3.writeInstalled(t, mgr.PluginDirectory())
	plugins := mgr.ReloadAllPlugins(t.Context())
	require.Len(t, plugins, 1)
	require.Equal(t, lifecycleGuestName, plugins[0].Name)
	require.Equal(t, v3.version, plugins[0].Version)
	require.Equal(t, StatusRunning, plugins[0].Status)
	require.True(t, second.IsClosed())
	third := assertLifecycleGuestRunning(t, mgr, v3)
	mgr.Stop()
	require.True(t, third.IsClosed())
	assertLifecycleGuestInactive(t, mgr, StatusUnloaded)
	assertLifecycleWrites(t, recorder,
		"init:v1", "start:v1", "stop:v1", "init:v2", "start:v2", "stop:v2", "init:v3", "start:v3", "stop:v3")
}

func TestManagerGuestLifecycleRollsBackAndRecoversFromGuestFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		guest     lifecycleGuest
		wantError string
	}{
		{"init rejection", lifecycleGuest{version: "broken", initError: "fixture rejected init"}, "init: fixture rejected init"},
		{"start trap", lifecycleGuest{version: "broken", trapStart: true}, "start: wasm error: unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, recorder := newLifecycleGuestManager(t)
			installed, err := mgr.InstallPluginArchive(t.Context(), tc.guest.archive(t), PluginInstallOptions{Enable: true})
			require.NoError(t, err)
			require.True(t, installed.Validation.Valid, "%+v", installed.Validation)
			require.Equal(t, StatusError, installed.Status)
			require.Contains(t, installed.Plugin.Error, tc.wantError)
			assertLifecycleGuestInactive(t, mgr, StatusError)
			health, _ := mgr.GetPluginHealth(lifecycleGuestName)
			require.Contains(t, health.Error, tc.wantError)
			assertLifecycleWrites(t, recorder, "init:broken")

			fixed := lifecycleGuest{version: "fixed"}
			fixed.writeInstalled(t, mgr.PluginDirectory())
			require.NoError(t, mgr.ReloadPlugin(lifecycleGuestName))
			assertLifecycleGuestRunning(t, mgr, fixed)
			assertLifecycleWrites(t, recorder, "init:broken", "init:fixed", "start:fixed")
		})
	}
}

type lifecycleWrite struct{ key, value string }

type lifecycleGuestRecorder struct {
	RedisClient
	writes []lifecycleWrite
}

func (r *lifecycleGuestRecorder) Set(key, value string, _ int64) error {
	r.writes = append(r.writes, lifecycleWrite{key, value})
	return nil
}

func newLifecycleGuestManager(t *testing.T) (*Manager, *lifecycleGuestRecorder) {
	t.Helper()
	root := t.TempDir()
	// The runtime mounts a relative data directory for each guest instance.
	t.Chdir(root)
	recorder := &lifecycleGuestRecorder{}
	mgr := NewManager(&HostEnv{
		Config: config.Config{Plugin: config.PluginConfig{Directory: filepath.Join(root, "installed"), Enabled: true}},
		Redis:  recorder,
	})
	mgr.Start()
	t.Cleanup(mgr.Stop)
	return mgr, recorder
}

func assertLifecycleWrites(t *testing.T, recorder *lifecycleGuestRecorder, phases ...string) {
	t.Helper()
	want := make([]lifecycleWrite, 0, len(phases))
	for _, phase := range phases {
		want = append(want, lifecycleWrite{"plugin:" + lifecycleGuestName + ":lifecycle", phase})
	}
	require.Equal(t, want, recorder.writes)
}

func assertLifecycleGuestInactive(t *testing.T, mgr *Manager, status PluginStatus) {
	t.Helper()
	health, ok := mgr.GetPluginHealth(lifecycleGuestName)
	require.True(t, ok)
	require.Equal(t, status, health.Status)
	require.False(t, health.Ready)
	require.Empty(t, mgr.ListPluginRoutes(lifecycleGuestName))
	require.Empty(t, mgr.ListPluginMiddleware(lifecycleGuestName))
	require.Empty(t, mgr.PendingRoutes())
	require.Empty(t, mgr.PendingMiddleware())
	_, err := mgr.CallPlugin(t.Context(), lifecycleGuestName, "handle", &pluginv1.HandleRequest{})
	require.ErrorContains(t, err, "not found or not ready")
}

func assertLifecycleGuestRunning(t *testing.T, mgr *Manager, guest lifecycleGuest) api.Module {
	t.Helper()
	health, ok := mgr.GetPluginHealth(lifecycleGuestName)
	require.True(t, ok)
	require.Equal(t, StatusRunning, health.Status, health.Error)
	require.True(t, health.Ready)
	info, ok := mgr.GetInstalledPluginInfo(lifecycleGuestName)
	require.True(t, ok)
	require.Equal(t, guest.version, info.Version)

	route := RouteRegistration{PluginName: lifecycleGuestName, Method: "GET", Path: "/" + guest.version, Handler: "handle"}
	middleware := MiddlewareRegistration{PluginName: lifecycleGuestName, Name: "guard-" + guest.version, Handler: "guard"}
	require.Equal(t, []RouteRegistration{route}, mgr.ListPluginRoutes(lifecycleGuestName))
	require.Equal(t, []MiddlewareRegistration{middleware}, mgr.ListPluginMiddleware(lifecycleGuestName))
	require.Equal(t, []RouteRegistration{route}, mgr.PendingRoutes())
	require.Equal(t, []MiddlewareRegistration{middleware}, mgr.PendingMiddleware())

	ctx, cancel := context.WithTimeout(t.Context(), mgr.RequestTimeout())
	defer cancel()
	request := &pluginv1.HandleRequest{Method: route.Method, Path: route.Path}
	response, err := mgr.CallPlugin(ctx, lifecycleGuestName, route.Handler, request)
	require.NoError(t, err)
	require.Equal(t, int32(200), response.GetStatus())
	require.Equal(t, guest.version, string(response.GetBody()))
	guard, err := mgr.CallPluginMiddleware(ctx, lifecycleGuestName, middleware.Handler, request)
	require.NoError(t, err)
	require.Equal(t, "next", guard.GetAction())

	pool := mgr.GetPlugin(lifecycleGuestName).Pool
	mod, err := pool.Get(ctx)
	require.NoError(t, err)
	pool.Put(mod)
	return mod
}

type lifecycleGuest struct {
	version            string
	initError          string
	trapStart          bool
	middlewareResponse *pluginv1.MiddlewareResponse
}

// StartDispatchTestGuest exposes the running test guest to external HTTP integration tests.
func StartDispatchTestGuest(t *testing.T, middleware *pluginv1.MiddlewareResponse) (*Manager, func() *pluginv1.HandleRequest) {
	t.Helper()
	mgr, _ := newLifecycleGuestManager(t)
	guest := lifecycleGuest{version: "catalog", middlewareResponse: middleware}
	installed, err := mgr.InstallPluginArchive(t.Context(), guest.archive(t), PluginInstallOptions{Enable: true})
	require.NoError(t, err)
	require.Equal(t, StatusRunning, installed.Status, installed.Plugin.Error)
	return mgr, func() *pluginv1.HandleRequest {
		t.Helper()
		pool := mgr.GetPlugin(lifecycleGuestName).Pool
		mod, err := pool.Get(t.Context())
		require.NoError(t, err)
		defer pool.Put(mod)
		size, ok := mod.Memory().ReadUint32Le(guestScratch - 4)
		require.True(t, ok)
		if size == 0 {
			return nil
		}
		data, ok := mod.Memory().Read(guestScratch, size)
		require.True(t, ok)
		var request pluginv1.HandleRequest
		require.NoError(t, proto.Unmarshal(data, &request))
		return &request
	}
}

func (g lifecycleGuest) manifest() string {
	return fmt.Sprintf("name: %s\nversion: %s\nmain: bin/guest.wasm\npermissions: [http_routes, middleware, redis]\n", lifecycleGuestName, g.version)
}

func (g lifecycleGuest) archive(t *testing.T) *bytes.Reader {
	t.Helper()
	return bytes.NewReader(makePluginArchive(t, map[string]string{
		"package/plugin.yaml":    g.manifest(),
		"package/bin/guest.wasm": string(g.wasm(t)),
	}))
}

func (g lifecycleGuest) writeInstalled(t *testing.T, dir string) {
	t.Helper()
	root := filepath.Join(dir, lifecycleGuestName)
	require.NoError(t, os.WriteFile(filepath.Join(root, "plugin.yaml"), []byte(g.manifest()), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "bin", "guest.wasm"), g.wasm(t), 0600))
}

// wasm assembles a small guest with static protobuf messages and one scratch buffer for ABI allocations.
func (g lifecycleGuest) wasm(t *testing.T) []byte {
	t.Helper()
	const (
		i32                = 0x7f
		i64                = 0x7e
		abi                = 0 // (i32, i32) -> i64
		allocation         = 1 // (i32) -> i32
		lifecycle          = 2 // () -> ()
		registerRoute      = 0
		registerMiddleware = 1
		redisSet           = 2
	)
	module := []byte("\x00asm\x01\x00\x00\x00")
	section := func(id byte, contents []byte) {
		module = append(module, id)
		module = appendWasmLengthAndBytes(module, contents)
	}
	section(1, []byte{3, 0x60, 2, i32, i32, 1, i64, 0x60, 1, i32, 1, i32, 0x60, 0, 0})
	imports := []byte{3}
	for _, name := range []string{"host_register_route", "host_register_middleware", "host_redis_set"} {
		imports = appendWasmLengthAndBytes(imports, []byte("env"))
		imports = appendWasmLengthAndBytes(imports, []byte(name))
		imports = append(imports, 0, abi) // function import and its type index
	}
	section(2, imports)
	section(3, []byte{6, allocation, abi, lifecycle, lifecycle, abi, abi})
	section(5, []byte{1, 0, 1}) // One memory with an initial size of one page.
	exports := []byte{7}
	exports = appendWasmLengthAndBytes(exports, []byte("memory"))
	exports = append(exports, 2, 0)
	for index, name := range []string{"allocate", "init", "start", "stop", "handle", "guard"} {
		exports = appendWasmLengthAndBytes(exports, []byte(name))
		exports = append(exports, 0, byte(index+3)) // Local functions follow the three imports.
	}
	section(7, exports)

	var messages []byte
	store := func(message proto.Message) (int64, int64) {
		data, err := proto.Marshal(message)
		require.NoError(t, err)
		ptr := len(messages)
		messages = append(messages, data...)
		return int64(ptr), int64(len(data))
	}
	callHost := func(code []byte, index byte, request proto.Message) []byte {
		ptr, size := store(request)
		code = appendWasmConst(code, 0x41, ptr)
		code = appendWasmConst(code, 0x41, size)
		return append(code, 0x10, index, 0x1a) // call host import, drop its return value
	}
	respond := func(code []byte, response proto.Message) []byte {
		ptr, size := store(response)
		return appendWasmConst(code, 0x42, ptr<<32|size)
	}
	mark := func(code []byte, phase string) []byte {
		return callHost(code, redisSet, &pluginv1.RedisSetRequest{Key: "lifecycle", Value: phase + ":" + g.version})
	}

	allocate := appendWasmConst(nil, 0x41, guestScratch)
	init := callHost(nil, registerRoute, &pluginv1.RegisterRouteRequest{Method: "GET", Path: "/" + g.version, Handler: "handle"})
	if g.middlewareResponse != nil {
		init = callHost(init, registerRoute, &pluginv1.RegisterRouteRequest{Method: "POST", Path: "/report", Handler: "handle", Middleware: []string{"auth", "guard-" + g.version}})
	}
	init = callHost(init, registerMiddleware, &pluginv1.RegisterMiddlewareRequest{Name: "guard-" + g.version, Handler: "guard"})
	init = respond(mark(init, "init"), &pluginv1.BoolResult{Success: g.initError == "", Error: g.initError})
	start := []byte{0x00} // unreachable
	if !g.trapStart {
		start = mark(nil, "start")
	}
	stop := mark(nil, "stop")
	handleResponse := &pluginv1.HandleResponse{Status: 200, Body: []byte(g.version)}
	guardResponse := &pluginv1.MiddlewareResponse{Action: "next"}
	var handle []byte
	if g.middlewareResponse != nil {
		// Save the received protobuf length beside its buffer only when the handler runs.
		handle = appendWasmConst(nil, 0x41, guestScratch-4)
		handle = append(handle, 0x20, 0x01, 0x36, 0x02, 0x00) // local.get 1; i32.store
		handleResponse = &pluginv1.HandleResponse{Status: 202, Headers: map[string]string{"X-Report-Version": g.version}, Body: []byte("report accepted")}
		guardResponse = g.middlewareResponse
	}
	handle = respond(handle, handleResponse)
	guard := respond(nil, guardResponse)
	code := []byte{6}
	for _, instructions := range [][]byte{allocate, init, start, stop, handle, guard} {
		body := append([]byte{0}, instructions...) // No local declarations.
		code = appendWasmLengthAndBytes(code, append(body, 0x0b))
	}
	section(10, code)
	require.LessOrEqual(t, len(messages), guestScratch-4)
	section(11, appendWasmLengthAndBytes([]byte{1, 0, 0x41, 0, 0x0b}, messages)) // Active data segment at memory offset 0.
	return module
}

func appendWasmLengthAndBytes(dst, data []byte) []byte {
	return append(binary.AppendUvarint(dst, uint64(len(data))), data...)
}

func appendWasmConst(dst []byte, opcode byte, value int64) []byte {
	dst = append(dst, opcode)
	// WASM integer constants use signed LEB128, unlike the section lengths.
	for {
		low := byte(value & 0x7f)
		value >>= 7
		if (value == 0 && low&0x40 == 0) || (value == -1 && low&0x40 != 0) {
			return append(dst, low)
		}
		dst = append(dst, low|0x80)
	}
}
