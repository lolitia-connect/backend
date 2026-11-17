package trafficagg

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/trafficbatch"
	"github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/internal/repository"
)

type Delta struct {
	ServerID       int64
	SubscriptionID int64
	Upload         int64
	Download       int64
}

func ApplyBatch(ctx context.Context, store repository.Store, key string, at time.Time, deltas []Delta) error {
	if key == "" || len(key) > 96 {
		return fmt.Errorf("invalid traffic batch identifier")
	}
	ordered := append([]Delta(nil), deltas...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].SubscriptionID == ordered[right].SubscriptionID {
			return ordered[left].ServerID < ordered[right].ServerID
		}
		return ordered[left].SubscriptionID < ordered[right].SubscriptionID
	})
	err := store.InTx(ctx, func(tx repository.Store) error {
		client := tx.Ent()
		processed, err := client.TrafficBatch.Query().Where(trafficbatch.ID(key)).Exist(ctx)
		if err != nil || processed {
			return err
		}
		if err := client.TrafficBatch.Create().SetID(key).Exec(ctx); err != nil {
			return err
		}
		logs := make([]*ent.TrafficLogCreate, 0, 200)
		for _, delta := range ordered {
			if delta.ServerID <= 0 || delta.SubscriptionID <= 0 || delta.Upload < 0 || delta.Download < 0 {
				return fmt.Errorf("invalid traffic delta")
			}
			query := client.UserSubscribe.Query().Where(usersubscribe.ID(delta.SubscriptionID))
			if client.Driver().Dialect() != dialect.SQLite {
				query = query.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
			}
			subscription, err := query.Only(ctx)
			if ent.IsNotFound(err) {
				continue
			}
			if err != nil {
				return err
			}
			upload, download := subscription.Upload, subscription.Download
			if subscription.Status == 3 {
				upload, download = subscription.ExpiredUpload, subscription.ExpiredDownload
			}
			if upload < 0 || download < 0 || delta.Upload > math.MaxInt64-upload || delta.Download > math.MaxInt64-download {
				return fmt.Errorf("traffic counter overflow for subscription %d", subscription.ID)
			}
			update := client.UserSubscribe.UpdateOneID(subscription.ID)
			if subscription.Status == 3 {
				update.AddExpiredUpload(delta.Upload).AddExpiredDownload(delta.Download)
			} else {
				update.AddUpload(delta.Upload).AddDownload(delta.Download)
			}
			if err := update.Exec(ctx); err != nil {
				return err
			}
			logs = append(logs, client.TrafficLog.Create().SetServerID(delta.ServerID).SetSubscribeID(subscription.ID).
				SetUserID(subscription.UserID).SetUpload(delta.Upload).SetDownload(delta.Download).SetTimestamp(at))
			if len(logs) == cap(logs) {
				if err := client.TrafficLog.CreateBulk(logs...).Exec(ctx); err != nil {
					return err
				}
				logs = logs[:0]
			}
		}
		if len(logs) > 0 {
			return client.TrafficLog.CreateBulk(logs...).Exec(ctx)
		}
		return nil
	})
	if err != nil && ent.IsConstraintError(err) {
		processed, checkErr := store.Ent().TrafficBatch.Query().Where(trafficbatch.ID(key)).Exist(ctx)
		if checkErr == nil && processed {
			return nil
		}
	}
	return err
}
