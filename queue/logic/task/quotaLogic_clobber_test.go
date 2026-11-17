package task

import (
	"context"
	"testing"
)

// TestWholeRowSaveWouldHaveClobbered proves the regression assertions above are
// discriminating rather than vacuous: it reproduces the pre-fix write (a
// whole-row `UpdateSubscribe` of the snapshot taken before the transaction) and
// shows it reverting the concurrently settled traffic and rotated credential.
//
// This test asserts the BUG, so it documents why the fix is necessary. It is
// deliberately self-contained and does not call the production grant path: if
// `ApplyQuotaGrant` ever regressed to a whole-row save, the sibling tests would
// fail here first.
func TestWholeRowSaveWouldHaveClobbered(t *testing.T) {
	service, client := quotaFixture(t)
	ctx := context.Background()
	seedSubscription(t, client, 1, nil)
	seedTask(t, client, 1)

	logic := NewQuotaTaskLogic(service)
	snapshot, err := logic.getSubscribes(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}

	// Concurrent traffic settlement + credential rotation after the snapshot.
	client.UserSubscribe.UpdateOneID(1).SetUpload(150).SetToken("T2").SetUUID("U2").ExecX(ctx)

	// The old behaviour: mutate the snapshot and save the whole row back.
	stale := *snapshot[0]
	stale.ExpireTime = stale.ExpireTime.AddDate(0, 0, 10)
	if err := service.Store.User().UpdateSubscribe(ctx, &stale); err != nil {
		t.Fatal(err)
	}

	stored := client.UserSubscribe.GetX(ctx, 1)
	if stored.Upload == 150 && stored.Token == "T2" {
		t.Fatal("a whole-row save no longer clobbers; this test no longer documents the defect — revisit the fix")
	}
	if stored.Upload != 100 || stored.Token != "T1" {
		t.Fatalf("unexpected clobber shape: upload=%d token=%q", stored.Upload, stored.Token)
	}
	t.Logf("confirmed: whole-row save reverted upload 150->%d and token T2->%q", stored.Upload, stored.Token)
}
