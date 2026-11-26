package initialize_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/stretchr/testify/require"
)

func TestInstalledPanelRestoresAdministratorAndSettingsOnRestart(t *testing.T) {
	t.Setenv("GATEWAY_MODE", "false")
	for _, backend := range []struct{ driver, env string }{{"mysql", "PPANEL_TEST_MYSQL_DSN"}, {"postgres", "PPANEL_TEST_POSTGRES_DSN"}} {
		t.Run(backend.driver, func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if dsn == "" {
				t.Skip("set " + backend.env + " to run the installation test")
			}
			ctx := t.Context()
			settings := orm.ParseDSN(dsn)
			require.NotNil(t, settings)
			adminDB, err := sql.Open(backend.driver, dsn)
			require.NoError(t, err)
			t.Cleanup(func() { _ = adminDB.Close() })
			name := fmt.Sprintf("ppanel_install_test_%d", time.Now().UnixNano())
			_, err = adminDB.ExecContext(ctx, "CREATE DATABASE "+name)
			require.NoError(t, err)
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				_, err := adminDB.ExecContext(cleanup, "DROP DATABASE "+name)
				require.NoError(t, err)
			})
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("Port: 0\n"), 0600))
			host, port, err := net.SplitHostPort(settings.Addr)
			require.NoError(t, err)
			redisHost, redisPort, err := net.SplitHostPort(miniredis.RunT(t).Addr())
			require.NoError(t, err)
			const password = "installation-test-password"
			form := map[string]string{
				"adminEmail": "Installer@Example.test", "adminPassword": password,
				"databaseDriver": backend.driver, "mysqlHost": host, "mysqlPort": port, "mysqlDatabase": name,
				"mysqlUser": settings.Username, "mysqlPassword": settings.Password, "redisHost": redisHost, "redisPort": redisPort,
			}
			installed, wizard, err := initialize.Config(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = wizard.Close() })
			origin := "http://" + wizard.Addr
			web := &http.Client{Timeout: 3 * time.Minute}
			t.Cleanup(web.CloseIdleConnections)
			probeDatabase := func(secret string, wantOK bool, wantMessage string) {
				t.Helper()
				data, err := json.Marshal(map[string]string{"driver": backend.driver, "host": host, "port": port, "database": name, "user": settings.Username, "password": secret})
				require.NoError(t, err)
				resp, err := web.Post(origin+"/init/database/test", "application/json", bytes.NewReader(data))
				require.NoError(t, err)
				defer resp.Body.Close()
				var result struct {
					Status  *bool  `json:"status"`
					Message string `json:"msg"`
				}
				require.Equal(t, http.StatusOK, resp.StatusCode)
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
				require.NotNil(t, result.Status)
				require.Equal(t, wantOK, *result.Status)
				require.Equal(t, wantMessage, result.Message)
			}
			page, err := web.Get(origin + "/")
			require.NoError(t, err)
			html, err := io.ReadAll(page.Body)
			_ = page.Body.Close()
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, page.StatusCode)
			require.Equal(t, "text/html; charset=utf-8", page.Header.Get("Content-Type"))
			require.Equal(t, "/init", page.Request.URL.Path)
			require.Contains(t, string(html), `id="setupForm"`)
			probeDatabase(settings.Password+"-incorrect", false, "Database connection failed")
			probeDatabase(settings.Password, true, "")
			submit := func(body []byte, wantStatus int) string {
				t.Helper()
				response, err := web.Post(origin+"/init/config", "application/json", bytes.NewReader(body))
				require.NoError(t, err)
				defer response.Body.Close()
				var result struct {
					Message string `json:"msg"`
				}
				require.Equal(t, wantStatus, response.StatusCode)
				require.NoError(t, json.NewDecoder(response.Body).Decode(&result))
				return result.Message
			}
			require.Equal(t, "Invalid request", submit([]byte(`{"adminEmail":`), http.StatusBadRequest))
			for _, invalid := range []struct{ field, value, reason string }{
				{"adminEmail", "installer", "a valid administrator email"},
				{"databaseDriver", "unknown", "unsupported database driver"},
			} {
				original := form[invalid.field]
				form[invalid.field] = invalid.value
				body, err := json.Marshal(form)
				require.NoError(t, err)
				require.Contains(t, submit(body, http.StatusBadRequest), invalid.reason)
				form[invalid.field] = original
			}
			unchanged, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "Port: 0\n", string(unchanged))
			body, err := json.Marshal(form)
			require.NoError(t, err)
			require.Equal(t, "Configuration initialized", submit(body, http.StatusOK))
			select {
			case ok := <-installed:
				require.True(t, ok)
			case <-time.After(5 * time.Second):
				t.Fatal("wizard did not hand off the installed configuration")
			}
			probeDatabase(settings.Password, false, "The database contains existing data. Please clear it before proceeding with the installation.")
			require.NoError(t, wizard.Shutdown(ctx))
			var saved config.Config
			require.NoError(t, conf.Load(path, &saved))
			require.Equal(t, name, saved.DatabaseConfig().Dbname)
			require.Equal(t, "installer@example.test", saved.Administrator.Email)
			require.Empty(t, saved.Administrator.Password)
			saved.GeoIP = config.GeoIPConfig{Path: filepath.Join(dir, "optional.mmdb")}
			saved.Debug = true
			saved.Host, port, err = net.SplitHostPort(wizard.Addr)
			require.NoError(t, err)
			saved.Port, err = strconv.Atoi(port)
			require.NoError(t, err)
			first := svc.NewServiceContext(saved)
			t.Cleanup(func() { require.NoError(t, first.Close()) })
			initialize.StartInitSystemConfig(first)
			owner := first.Ent.User.Query().OnlyX(ctx)
			require.True(t, owner.IsAdmin && tool.VerifyPassWord(password, owner.Password))
			identity := first.Ent.UserAuthMethod.Query().OnlyX(ctx)
			require.Equal(t, owner.ID, identity.UserID)
			require.True(t, identity.Verified)
			require.Equal(t, saved.Administrator.Email, identity.AuthIdentifier)
			for _, field := range []struct{ category, key, value, kind string }{
				{"site", "SiteName", "Customer portal", "string"}, {"currency", "CurrencyUnit", "EUR", "string"}, {"register", "DeviceLimit", "7", "int"},
			} {
				require.NoError(t, first.Store.System().UpsertByCategoryKey(ctx, field.category, field.key, field.value, field.kind, ""))
			}
			region := first.Ent.NodeGroup.Create().SetName("Regional servers").SaveX(ctx)
			other := first.Ent.NodeGroup.Create().SetName("Other servers").SaveX(ctx)
			primary := first.Ent.Subscribe.Create().SetName("Primary region").SetNodeGroupID(region.ID).SaveX(ctx)
			backup := first.Ent.Subscribe.Create().SetName("Backup region").SetNodeGroupID(other.ID).SetNodeGroupIds([]int64{region.ID}).SaveX(ctx)
			first.Ent.Subscribe.Create().SetName("Other region").SetNodeGroupID(other.ID).SetNodeGroupIds([]int64{other.ID}).SaveX(ctx)
			edge := first.Ent.Server.Create().SetName("Regional edge").SaveX(ctx)
			matched := first.Ent.Node.Create().SetServerID(edge.ID).SetName("Regional member").SetTags("shared,region_1").SetNodeGroupIds([]int64{other.ID, region.ID}).SaveX(ctx)
			first.Ent.Node.Create().SetServerID(edge.ID).SetName("Similar tag").SetTags("shared,regionX1").SetNodeGroupIds([]int64{region.ID}).SaveX(ctx)
			first.Ent.Node.Create().SetServerID(edge.ID).SetName("Other group").SetTags("region_1").SetNodeGroupIds([]int64{other.ID}).SaveX(ctx)
			require.NoError(t, first.Close())
			restarted := svc.NewServiceContext(saved)
			t.Cleanup(func() { require.NoError(t, restarted.Close()) })
			panel := internal.NewService(restarted)
			stopped := make(chan struct{})
			var runErr error
			go func() {
				defer close(stopped)
				runErr = panel.Run()
			}()
			t.Cleanup(func() {
				panel.Stop()
				select {
				case <-stopped:
					require.NoError(t, runErr)
				case <-time.After(10 * time.Second):
					t.Error("panel did not stop")
				}
			})
			awaitSite := func(name string) {
				t.Helper()
				client := &http.Client{Timeout: time.Second}
				defer client.CloseIdleConnections()
				require.Eventually(t, func() bool {
					resp, err := client.Get(origin + "/v1/common/site/config")
					if err != nil {
						return false
					}
					defer resp.Body.Close()
					var result struct {
						Code int                           `json:"code"`
						Data types.GetGlobalConfigResponse `json:"data"`
					}
					return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&result) == nil && result.Code == http.StatusOK && result.Data.Site.SiteName == name
				}, 30*time.Second, 100*time.Millisecond, "site name %q", name)
			}
			awaitSite("Customer portal")
			require.NoError(t, restarted.Store.System().UpsertByCategoryKey(ctx, "site", "SiteName", "Renamed portal", "string", ""))
			require.NoError(t, panel.Restart())
			awaitSite("Renamed portal")
			total, plans, err := restarted.Store.Subscribe().FilterListByNodeGroups(ctx, &subscribe.FilterByNodeGroupsParams{NodeGroupIds: []int64{region.ID}})
			require.NoError(t, err)
			require.EqualValues(t, 2, total)
			var planIDs []int64
			for _, plan := range plans {
				planIDs = append(planIDs, plan.Id)
			}
			require.ElementsMatch(t, []int64{primary.ID, backup.ID}, planIDs)
			nodeTotal, nodes, err := restarted.Store.Node().FilterNodeList(ctx, &node.FilterNodeParams{
				Page: 1, Size: 10, Tag: []string{"region_1"}, NodeGroupIds: []int64{region.ID},
			})
			require.NoError(t, err)
			require.EqualValues(t, 1, nodeTotal)
			require.Len(t, nodes, 1)
			require.Equal(t, matched.ID, nodes[0].Id)
			groupSize, err := restarted.Store.Node().CountByNodeGroupId(ctx, region.ID)
			require.NoError(t, err)
			require.EqualValues(t, 2, groupSize)
			require.Equal(t, owner.Password, restarted.Ent.User.Query().OnlyX(ctx).Password)
			require.Equal(t, "Renamed portal", restarted.Config.Site.SiteName)
			require.Equal(t, "EUR", restarted.Config.Currency.Unit)
			require.EqualValues(t, 7, restarted.Config.Register.DeviceLimit)
			require.NotEmpty(t, first.Config.Node.NodeSecret)
			require.Equal(t, first.Config.Node.NodeSecret, restarted.Config.Node.NodeSecret)
		})
	}
}
