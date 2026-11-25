package group

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/migrate"
	"github.com/perfect-panel/server/internal/repository"
)

func groupLockDSN(t *testing.T, driver, specificEnv, commonEnv string) string {
	t.Helper()
	if dsn := os.Getenv(specificEnv); dsn != "" {
		return dsn
	}
	dsn := os.Getenv(commonEnv)
	if dsn == "" {
		t.Skipf("set %s or %s to run the row lock test", commonEnv, specificEnv)
	}
	name := fmt.Sprintf("ppanel_group_test_%d", time.Now().UnixNano())
	var isolated string
	if driver == dialect.MySQL {
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.DBName = name
		isolated = cfg.FormatDSN()
	} else {
		u, err := url.Parse(dsn)
		if err != nil || u.Host == "" {
			t.Fatalf("%s must be a PostgreSQL URL", commonEnv)
		}
		u.Path = "/" + name
		isolated = u.String()
	}
	admin, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+name); err != nil {
			t.Error(err)
		}
	})
	return isolated
}

func TestGroupingLocksSubscriptionAgainstConcurrentWriter(t *testing.T) {
	for _, tc := range []struct{ driver, groupEnv, commonEnv string }{
		{dialect.MySQL, "PPANEL_GROUP_TEST_MYSQL_DSN", "PPANEL_TEST_MYSQL_DSN"},
		{dialect.Postgres, "PPANEL_GROUP_TEST_POSTGRES_DSN", "PPANEL_TEST_POSTGRES_DSN"},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			dsn := groupLockDSN(t, tc.driver, tc.groupEnv, tc.commonEnv)
			db, err := sql.Open(tc.driver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			client := ent.NewClient(ent.Driver(entsql.OpenDB(tc.driver, db)))
			ctx := t.Context()
			if err := migrate.Create(ctx, client.Schema, []*schema.Table{migrate.UserSubscribeTable}); err != nil {
				t.Fatal(err)
			}
			sub := client.UserSubscribe.Create().SetUserID(1).SetOrderID(1).SetSubscribeID(1).SetStatus(1).SaveX(ctx)
			defer client.UserSubscribe.DeleteOneID(sub.ID).Exec(ctx)
			store := repository.NewStore(client, nil)
			err = store.InTx(ctx, func(tx repository.Store) error {
				if _, err := tx.User().FindUnlockedSubscribesForGroupUpdate(ctx, 0, 1); err != nil {
					return err
				}
				// A second connection must not be able to acquire the writer's row lock yet.
				query := "SELECT id FROM user_subscribe WHERE id = ? FOR UPDATE NOWAIT"
				if tc.driver == dialect.Postgres {
					query = "SELECT id FROM user_subscribe WHERE id = $1 FOR UPDATE NOWAIT"
				}
				var id int64
				err := db.QueryRowContext(ctx, query, sub.ID).Scan(&id)
				var myErr *mysql.MySQLError
				var pgErr *pq.Error
				if !(errors.As(err, &myErr) && (myErr.Number == 1205 || myErr.Number == 3572) || errors.As(err, &pgErr) && pgErr.Code == "55P03") {
					t.Errorf("concurrent writer should encounter a locked row: %v", err)
				}
				return tx.User().UpdateUserSubscribeNodeGroup(ctx, sub.ID, 2)
			})
			if err != nil {
				t.Fatal(err)
			}
			client.UserSubscribe.UpdateOneID(sub.ID).AddUpload(7).ExecX(ctx)
			row := client.UserSubscribe.GetX(ctx, sub.ID)
			if row.Upload != 7 || row.NodeGroupID != 2 {
				t.Fatalf("assignment=%d upload=%d", row.NodeGroupID, row.Upload)
			}
		})
	}
}
