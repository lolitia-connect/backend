package user

import (
	"strconv"
	"testing"
	"time"

	modeluser "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/stretchr/testify/require"
)

func TestAdminCannotResetTrafficOrDeleteProviderManagedSubscription(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	ctx := t.Context()
	plan := client.Subscribe.Create().SetName("Provider plan").SaveX(ctx)
	sub := client.UserSubscribe.Create().SetUserID(7).SetSubscribeID(plan.ID).SetOrderID(1).
		SetEntitlementSource("provider").SetStatus(1).SetUpload(120).SetDownload(240).SaveX(ctx)

	reset := NewResetUserSubscribeTrafficLogic(ctx, fixture.svcCtx)
	require.ErrorIs(t, reset.ResetUserSubscribeTraffic(&types.ResetUserSubscribeTrafficRequest{UserSubscribeId: sub.ID}), modeluser.ErrProviderManaged)
	remove := NewDeleteUserSubscribeLogic(ctx, fixture.svcCtx)
	require.ErrorIs(t, remove.DeleteUserSubscribe(&types.DeleteUserSubscribeRequest{UserSubscribeId: sub.ID}), modeluser.ErrProviderManaged)

	stored := client.UserSubscribe.GetX(ctx, sub.ID)
	require.Equal(t, sub.EntitlementSource, stored.EntitlementSource)
	require.Equal(t, sub.Status, stored.Status)
	require.Equal(t, sub.Upload, stored.Upload)
	require.Equal(t, sub.Download, stored.Download)
}

func TestAdminBatchArchiveProtectsDemoAdminAndPreservesWallet(t *testing.T) {
	fixture, client, request := basicInfoFixture(t, "")
	ctx := t.Context()
	demoAdmin := client.User.Create().SetID(2).SetPassword("fixture").SetIsAdmin(true).SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	archive := NewBatchDeleteUserLogic(ctx, fixture.svcCtx)
	memberID := strconv.FormatInt(request.UserId, 10)
	batch := &types.BatchDeleteUserRequest{Ids: []string{"2", memberID}}

	t.Setenv("PPANEL_MODE", "demo")
	var problem *xerr.CodeError
	require.ErrorAs(t, archive.BatchDeleteUser(batch), &problem)
	require.EqualValues(t, 503, problem.GetErrCode())
	require.Nil(t, client.User.GetX(ctx, demoAdmin.ID).DeletedAt)
	require.Nil(t, client.User.GetX(ctx, request.UserId).DeletedAt)

	require.NoError(t, archive.BatchDeleteUser(&types.BatchDeleteUserRequest{Ids: []string{memberID, strconv.FormatInt(other.ID, 10)}}))
	require.NotNil(t, client.User.GetX(ctx, request.UserId).DeletedAt)
	require.NotNil(t, client.User.GetX(ctx, other.ID).DeletedAt)
	require.Nil(t, client.User.GetX(ctx, demoAdmin.ID).DeletedAt)
	wallet, err := fixture.svcCtx.Store.Wallet().FindOne(ctx, request.UserId)
	require.NoError(t, err)
	require.Equal(t, request.Balance, wallet.Balance)
	require.Equal(t, request.GiftAmount, wallet.GiftAmount)
	require.Equal(t, request.Commission, wallet.Commission)
}

func TestAdminResetTrafficFailurePreservesUsage(t *testing.T) {
	fixture, client, _ := basicInfoFixture(t, "")
	ctx := t.Context()
	plan := client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	sub := client.UserSubscribe.Create().SetUserID(7).SetSubscribeID(plan.ID).SetOrderID(1).
		SetExpireTime(time.Now().Add(time.Hour)).SetUpload(13).SetDownload(29).SaveX(ctx)
	require.NoError(t, client.Driver().Exec(ctx, `CREATE TRIGGER reject_traffic_reset BEFORE UPDATE OF upload, download ON user_subscribe BEGIN SELECT RAISE(ABORT, 'traffic reset failed'); END`, []any{}, nil))

	reset := NewResetUserSubscribeTrafficLogic(ctx, fixture.svcCtx)
	var problem *xerr.CodeError
	require.ErrorAs(t, reset.ResetUserSubscribeTraffic(&types.ResetUserSubscribeTrafficRequest{UserSubscribeId: sub.ID}), &problem)
	require.Equal(t, xerr.DatabaseUpdateError, problem.GetErrCode())
	stored := client.UserSubscribe.GetX(ctx, sub.ID)
	require.Equal(t, sub.Upload, stored.Upload)
	require.Equal(t, sub.Download, stored.Download)
}
