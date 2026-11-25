package plugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perfect-panel/server/internal/config"
	runtimeplugin "github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/hertzx"
)

func TestInstalledPluginCatalogUsesUploadedManifest(t *testing.T) {
	manager := runtimeplugin.NewManager(&runtimeplugin.HostEnv{
		Config: config.Config{Plugin: config.PluginConfig{Directory: t.TempDir(), Enabled: true}},
	})
	t.Cleanup(manager.Stop)
	service := &svc.ServiceContext{PluginMgr: manager}
	router := hertzx.New()
	const base = "/v1/admin/plugins"
	router.Hertz().POST(base+"/upload", InstalledUploadHandler(service))
	router.Hertz().GET(base, InstalledListHandler(service))
	router.Hertz().GET(base+"/:name", InstalledDetailHandler(service))
	router.Hertz().GET(base+"/:name/manifest", InstalledManifestHandler(service))
	router.Hertz().POST(base+"/:name/validate", InstalledValidateHandler(service))
	router.Hertz().GET(base+"/:name/routes", InstalledRoutesHandler(service))
	router.Hertz().GET(base+"/:name/middlewares", InstalledMiddlewareHandler(service))
	router.Hertz().GET(base+"/:name/events", InstalledEventsHandler(service))
	router.Hertz().GET(base+"/:name/health", InstalledHealthHandler(service))
	router.Hertz().POST(base+"/:name/archive", InstalledActionHandler(service, "archive"))

	call := func(method, path string, body *bytes.Buffer, contentType string, code int, out any) {
		t.Helper()
		request := httptest.NewRequestWithContext(t.Context(), method, path, body)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var envelope struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != http.StatusOK || envelope.Code != code {
			t.Fatalf("%s %s returned HTTP %d, %s, want API code %d: %v", method, path, response.Code, response.Body, code, err)
		}
		if out != nil {
			if err := json.Unmarshal(envelope.Data, out); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
		}
	}
	upload := func(name, version, description string, replace bool, code int) {
		t.Helper()
		var archive bytes.Buffer
		writer := zip.NewWriter(&archive)
		manifest, err := writer.Create("plugin.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(manifest, "name: %s\nversion: %q\ndescription: %q\nmain: plugin.wasm\npermissions: [logging]\n", name, version, description); err != nil {
			t.Fatal(err)
		}
		module, err := writer.Create("plugin.wasm")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := module.Write([]byte("\x00asm\x01\x00\x00\x00")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, err := form.CreateFormFile("file", "plugin.zip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = archive.WriteTo(file); err != nil {
			t.Fatal(err)
		}
		if replace {
			if err := form.WriteField("replace", "true"); err != nil {
				t.Fatal(err)
			}
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		call(http.MethodPost, base+"/upload", &body, form.FormDataContentType(), code, nil)
	}
	empty := bytes.NewBuffer(nil)
	call(http.MethodPost, base+"/upload", empty, "", http.StatusBadRequest, nil)
	upload("catalog", "1.0", "Regional catalog", false, http.StatusOK)
	upload("support", "1.0", "Regional support", false, http.StatusOK)
	upload("metrics", "1.0", "Metrics", false, http.StatusOK)
	upload("catalog", "2.0", "Regional catalog", false, http.StatusBadRequest)

	var detail runtimeplugin.PluginInfo
	call(http.MethodGet, base+"/catalog", empty, "", http.StatusOK, &detail)
	if detail.Version != "1.0" || detail.Status != runtimeplugin.StatusUnloaded {
		t.Fatalf("rejected duplicate changed installed plugin: %+v", detail)
	}
	upload("catalog", "2.0", "Regional catalog", true, http.StatusOK)
	var manifest runtimeplugin.PluginManifest
	call(http.MethodGet, base+"/catalog/manifest", empty, "", http.StatusOK, &manifest)
	if manifest.Name != "catalog" || manifest.Version != "2.0" || !manifest.HasPermission(runtimeplugin.PermLogging) {
		t.Fatalf("replacement did not persist the uploaded manifest: %+v", manifest)
	}
	var check runtimeplugin.PluginValidation
	call(http.MethodPost, base+"/catalog/validate", empty, "", http.StatusOK, &check)
	if !check.Valid {
		t.Fatalf("uploaded WASM failed validation: %s", check.Error)
	}
	var routes []runtimeplugin.RouteRegistration
	call(http.MethodGet, base+"/catalog/routes", empty, "", http.StatusOK, &routes)
	if len(routes) != 0 {
		t.Fatalf("unloaded plugin routes = %+v", routes)
	}
	var middleware []runtimeplugin.MiddlewareRegistration
	call(http.MethodGet, base+"/catalog/middlewares", empty, "", http.StatusOK, &middleware)
	if len(middleware) != 0 {
		t.Fatalf("unloaded plugin middleware = %+v", middleware)
	}
	var events []runtimeplugin.EventSubscription
	call(http.MethodGet, base+"/catalog/events", empty, "", http.StatusOK, &events)
	if len(events) != 0 {
		t.Fatalf("unloaded plugin events = %+v", events)
	}
	var health runtimeplugin.PluginHealth
	call(http.MethodGet, base+"/catalog/health", empty, "", http.StatusOK, &health)
	if health.Name != "catalog" || health.Status != runtimeplugin.StatusUnloaded || health.Ready {
		t.Fatalf("unloaded plugin health = %+v", health)
	}
	call(http.MethodPost, base+"/catalog/archive", empty, "", http.StatusBadRequest, nil)
	call(http.MethodGet, base+"/Bad", empty, "", http.StatusBadRequest, nil)
	var list pluginListResponse
	call(http.MethodGet, base+"?q=REGIONAL&status=unloaded&page=2&size=1", empty, "", http.StatusOK, &list)
	if list.Total != 2 || len(list.List) != 1 || list.List[0].Name != "support" {
		t.Fatalf("filtered plugin page = %+v", list)
	}
	call(http.MethodGet, base+"/unknown", empty, "", http.StatusNotFound, nil)
}
