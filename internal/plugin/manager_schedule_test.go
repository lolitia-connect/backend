package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/config"
)

func TestManagerRegistryAndInstalledPluginViews(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(&HostEnv{Config: config.Config{
		Host: "127.0.0.1", Port: 8080, Debug: true,
		Site:     config.SiteConfig{SiteName: "Panel", SiteDesc: "Description", SiteLogo: "logo", Host: "panel.example.test"},
		Currency: config.Currency{Unit: "CNY", Symbol: "¥"},
		Plugin:   config.PluginConfig{Directory: root, Enabled: false},
	}})
	defer manager.Stop()

	manager.plugins["zeta"] = &PluginInstance{Name: "zeta", Version: "2", Status: StatusRunning,
		Manifest: &PluginManifest{Permissions: []string{string(PermRedis), string(PermDatabaseRead)}},
		Routes:   []RouteRegistration{{PluginName: "zeta", Method: "GET", Path: "/ready", Handler: "handle"}},
		AsyncSem: make(chan struct{}, 3)}
	manager.plugins["alpha"] = &PluginInstance{Name: "alpha", Version: "1", Status: StatusError, Error: "broken",
		Manifest: &PluginManifest{Permissions: []string{string(PermHTTPRoutes)}}}
	manager.routes = map[string]RouteRegistration{
		routeKey("zeta", "GET", "/ready"):    {PluginName: "zeta", Method: "GET", Path: "/ready", Handler: "handle"},
		routeKey("zeta", "ANY", "/fallback"): {PluginName: "zeta", Method: "ANY", Path: "/fallback", Handler: "fallback"},
	}
	manager.middleware = map[string]MiddlewareRegistration{
		middlewareKey("zeta", "auth"): {PluginName: "zeta", Name: "auth", Handler: "guard"},
	}
	manager.pendingMiddleware = []MiddlewareRegistration{{PluginName: "zeta", Name: "auth", Handler: "guard"}}

	plugins := manager.ListPlugins()
	if len(plugins) != 2 || plugins[0].Name != "alpha" || plugins[1].Name != "zeta" || plugins[1].Routes[0] != "GET /ready" {
		t.Fatalf("sorted plugin list = %+v", plugins)
	}
	if !manager.HasPermission("zeta", PermDatabaseRead) || manager.HasPermission("zeta", PermDatabaseWrite) || manager.HasPermission("missing", PermRedis) {
		t.Fatal("plugin permission lookup returned the wrong result")
	}
	if route, ok := manager.FindRoute("zeta", "get", "ready"); !ok || route.Handler != "handle" {
		t.Fatalf("normalized route lookup = %+v, %t", route, ok)
	}
	if route, ok := manager.FindRoute("zeta", "POST", "/fallback"); !ok || route.Handler != "fallback" {
		t.Fatalf("ANY route lookup = %+v, %t", route, ok)
	}
	if _, ok := manager.FindRoute("zeta", "DELETE", "/missing"); ok {
		t.Fatal("missing plugin route was found")
	}
	if middleware, ok := manager.FindMiddleware("zeta", "auth"); !ok || middleware.Handler != "guard" {
		t.Fatalf("plugin middleware lookup = %+v, %t", middleware, ok)
	}
	if middleware, ok := manager.FindMiddleware("other", "auth"); !ok || middleware.PluginName != "zeta" {
		t.Fatalf("legacy middleware lookup = %+v, %t", middleware, ok)
	}

	for _, route := range []RouteRegistration{
		{}, {PluginName: "zeta", Method: "GET", Path: "/missing"},
		{PluginName: "zeta", Path: "/missing", Handler: "handle"},
	} {
		if err := manager.RegisterRoute(route); err == nil {
			t.Fatalf("invalid route %+v was accepted", route)
		}
	}
	validRoute := RouteRegistration{PluginName: "zeta", Method: "POST", Path: "reports", Handler: "report"}
	if err := manager.RegisterRoute(validRoute); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterRoute(validRoute); err != nil || len(manager.ListPluginRoutes("zeta")) != 3 {
		t.Fatal("duplicate route changed the registry")
	}
	for _, middleware := range []MiddlewareRegistration{
		{}, {PluginName: "zeta", Name: "auth"},
		{PluginName: "zeta", Name: "audit", Handler: ""},
	} {
		if err := manager.RegisterMiddleware(middleware); err == nil {
			t.Fatalf("invalid middleware %+v was accepted", middleware)
		}
	}
	validMiddleware := MiddlewareRegistration{PluginName: "zeta", Name: "audit", Handler: "audit"}
	if err := manager.RegisterMiddleware(validMiddleware); err != nil || len(manager.ListPluginMiddleware("zeta")) != 2 {
		t.Fatal(err)
	}

	if got := manager.PluginDirectory(); got != root {
		t.Fatalf("plugin directory = %q, want %q", got, root)
	}
	if got := manager.GetConfigValue("Site.SiteName"); got != "Panel" {
		t.Fatalf("site name = %q", got)
	}
	if got := manager.GetConfigValue("Site.SiteDesc"); got != "Description" {
		t.Fatalf("site description = %q", got)
	}
	if got := manager.GetConfigValue("Site.SiteLogo"); got != "logo" {
		t.Fatalf("site logo = %q", got)
	}
	if got := manager.GetConfigValue("Site.Host"); got != "panel.example.test" {
		t.Fatalf("site host = %q", got)
	}
	if got := manager.GetConfigValue("Currency.Unit"); got != "CNY" {
		t.Fatalf("currency unit = %q", got)
	}
	if got := manager.GetConfigValue("Currency.Symbol"); got != "¥" {
		t.Fatalf("currency symbol = %q", got)
	}
	if got := manager.GetConfigValue("Debug"); got != "true" {
		t.Fatalf("debug value = %q", got)
	}
	if got := manager.GetConfigValue("Host"); got != "127.0.0.1" {
		t.Fatalf("host value = %q", got)
	}
	if got := manager.GetConfigValue("Port"); got != "8080" {
		t.Fatalf("port value = %q", got)
	}
	if got := manager.GetConfigValue("Database.Password"); got != "" {
		t.Fatalf("unsafe config value = %q", got)
	}
	if manager.GetStore() != nil {
		t.Fatal("manager unexpectedly exposed a nil store")
	}

	installed := filepath.Join(root, "installed")
	if err := os.MkdirAll(filepath.Join(installed, "catalog"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "catalog", "plugin.yaml"), []byte("name: catalog\nversion: 1.2.3\nmain: bin/plugin.wasm\nauthor: Fixture\ndescription: Catalog plugin\npermissions: [config_read]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(installed, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "broken", "plugin.yaml"), []byte("name: broken\nversion: [invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "file-entry"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.config.Directory = installed
	list := manager.ListInstalledPlugins()
	if len(list) != 4 {
		t.Fatalf("installed plugin list = %+v", list)
	}
	if list[0].Name != "alpha" || list[1].Name != "broken" || list[1].Status != StatusError || list[2].Name != "catalog" || list[2].Status != StatusUnloaded || list[3].Name != "zeta" {
		t.Fatalf("installed plugin ordering/status = %+v", list)
	}
	info, ok := manager.GetInstalledPluginInfo("catalog")
	if !ok || info.Version != "1.2.3" || info.Status != StatusUnloaded {
		t.Fatalf("installed catalog info = %+v, %t", info, ok)
	}
	if _, ok := manager.GetInstalledPluginInfo("../catalog"); ok {
		t.Fatal("path traversal plugin info was returned")
	}
	manifest, err := manager.GetInstalledManifest("catalog")
	if err != nil || manifest.Name != "catalog" {
		t.Fatalf("installed manifest = %+v, %v", manifest, err)
	}
	if _, err := manager.GetInstalledManifest("broken"); err == nil {
		t.Fatal("invalid manifest was accepted")
	}
	if health, ok := manager.GetPluginHealth("zeta"); !ok || health.Ready || health.PoolSize != 0 || health.AsyncLimit != 3 || health.RegisteredRoute != 2 {
		t.Fatalf("running plugin health = %+v, %t", health, ok)
	}
	if health, ok := manager.GetPluginHealth("missing"); !ok || health.Status != StatusUnloaded {
		t.Fatalf("missing plugin health = %+v, %t", health, ok)
	}
	if health, ok := manager.GetPluginHealth("../bad"); ok || health.Status != StatusError {
		t.Fatalf("invalid plugin health = %+v, %t", health, ok)
	}
	if _, ok := manager.GetInstalledPluginInfo("broken"); ok {
		t.Fatal("broken plugin was reported as installed")
	}
	if manager.EventBus() == nil {
		t.Fatal("manager event bus is nil")
	}
}

func TestManagerSchedulesTasksAndSubscribesEvents(t *testing.T) {
	manager := NewManager(&HostEnv{Config: config.Config{Plugin: config.PluginConfig{Enabled: false}}})
	defer manager.Stop()
	for _, input := range [][4]string{{"", "job", "*/1 * * * *", "handle"}, {"demo", "", "*/1 * * * *", "handle"}, {"demo", "job", "", "handle"}, {"demo", "job", "*/1 * * * *", ""}} {
		if err := manager.ScheduleTask(input[0], input[1], input[2], input[3]); err == nil {
			t.Fatalf("invalid scheduled task %+v was accepted", input)
		}
	}
	if err := manager.ScheduleTask("demo", "job", "*/1 * * * *", "handle"); err != nil {
		t.Fatal(err)
	}
	if err := manager.ScheduleTask("demo", "job", "*/1 * * * *", "handle"); err != nil {
		t.Fatal(err)
	}
	if len(manager.scheduledTasks) != 1 {
		t.Fatalf("scheduled tasks = %d, want one deduplicated task", len(manager.scheduledTasks))
	}
	if err := manager.SubscribeEvent("demo", "order.paid", ""); err != nil {
		t.Fatal(err)
	}
	events := manager.ListPluginEvents("demo")
	if len(events) != 1 || events[0].Handler != "on_order_paid" {
		t.Fatalf("event subscriptions = %+v", events)
	}
	if err := manager.SubscribeEvent("", "order.paid", "handler"); err == nil {
		t.Fatal("empty plugin event subscription was accepted")
	}
	manager.removeCronTasks("demo")
	if len(manager.scheduledTasks) != 0 {
		t.Fatal("plugin cron task was not removed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := manager.WaitReady(ctx); err == nil {
		t.Fatal("unstarted manager reported ready")
	}
	manager.Start()
	if err := manager.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
}
