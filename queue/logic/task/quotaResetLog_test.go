package task

import (
	"context"
	"testing"

	"github.com/perfect-panel/server/ent"
	entsystemlog "github.com/perfect-panel/server/ent/systemlog"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/task"
)

// TestQuotaResetLogOnlyWhenCountersWereReset pins the audit invariant the
// ResetTraffic branch is supposed to uphold: a durable "quota traffic reset"
// log entry is evidence that a subscription's counters were zeroed.
//
// The reset log is written before the grant is applied, and a grant the CAS
// predicate refuses (status >= 4, provider-managed) becomes a soft
// per-subscription failure that still commits the transaction. The log must not
// survive that downgrade, or the audit trail claims a reset that never happened.
func TestQuotaResetLogOnlyWhenCountersWereReset(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := context.Background()

	// A terminal row: the grant CAS (ID + StatusLT(4) + EntitlementSourceEQ(""))
	// refuses it, but the reset log is inserted first.
	seedSubscription(t, client, 1, func(create *ent.UserSubscribeCreate) {
		create.SetStatus(4).SetUpload(500).SetDownload(600)
	})
	info := seedTask(t, client, 1)

	logic := NewQuotaTaskLogic(service)
	subscribes, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if err := logic.processSubscribes(ctx, subscribes, task.QuotaContent{ResetTraffic: true}, info); err != nil {
		t.Fatal(err)
	}

	stored := client.UserSubscribe.GetX(ctx, 1)
	logs := client.SystemLog.Query().Where(entsystemlog.TypeEQ(modellog.TypeResetSubscribe.Uint8())).CountX(ctx)

	reset := stored.Upload == 0 && stored.Download == 0
	if reset && logs == 0 {
		t.Fatal("counters were reset but no reset log was written")
	}
	if !reset && logs != 0 {
		t.Fatalf("an audit log claims a quota traffic reset, but the counters are unchanged "+
			"(upload=%d download=%d, status=%d) and %d reset log(s) were committed",
			stored.Upload, stored.Download, stored.Status, logs)
	}
}
