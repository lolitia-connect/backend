package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	entorder "github.com/perfect-panel/server/ent/order"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	worker "github.com/perfect-panel/server/queue/logic/order"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestRedeemingIssuedCodesGrantsAndExtendsOneSubscription(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	inspector := f.startQueue()
	_, admin := f.account(true)
	owner, customer := f.account(false)
	plan := f.client.Subscribe.Create().SetName("Trial access").SetSell(true).SetTraffic(1024).SaveX(ctx)
	const adminPath = "/v1/admin/redemption"
	const redeem = "/v1/public/redemption/"
	f.request(http.MethodPost, adminPath+"/code", admin, fmt.Sprintf(`{"total_count":2,"subscribe_plan":"%d","unit_time":"day","quantity":2,"batch_count":2}`, plan.ID), http.StatusOK)
	var issued types.GetRedemptionCodeListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, adminPath+"/code/list?page=1&size=10", admin, "", http.StatusOK), &issued))
	require.Len(t, issued.List, 2)
	var previousExpiry time.Time
	var subscriptionID int64
	for index, code := range issued.List {
		body := fmt.Sprintf(`{"code":%q}`, code.Code)
		if index == 0 {
			toggle := func(status int) {
				t.Helper()
				f.request(http.MethodPut, adminPath+"/code/status", admin, fmt.Sprintf(`{"id":"%d","status":%d}`, code.Id, status), http.StatusOK)
			}
			toggle(0)
			f.request(http.MethodPost, redeem, customer, body, xerr.InvalidParams)
			require.Zero(t, f.client.Order.Query().CountX(ctx))
			require.Zero(t, f.client.RedemptionRecord.Query().CountX(ctx))
			toggle(1)
		}
		f.request(http.MethodPost, redeem, customer, body, http.StatusOK)
		jobs, err := inspector.ListPendingTasks("default")
		require.NoError(t, err)
		require.Len(t, jobs, 1)
		job := jobs[0]
		require.Equal(t, queue.ForthwithActivateOrder, job.Type)
		var payload queue.ForthwithActivateOrderPayload
		require.NoError(t, json.Unmarshal(job.Payload, &payload))
		invoice := f.client.Order.Query().Where(entorder.OrderNo(payload.OrderNo)).OnlyX(ctx)
		require.Equal(t, owner.ID, invoice.UserID)
		require.EqualValues(t, order.StatusPaid, invoice.Status)
		require.Zero(t, invoice.Amount)
		activation := asynq.NewTask(job.Type, job.Payload)
		logic := worker.NewActivateOrderLogic(f.service)
		require.NoError(t, logic.ProcessTask(ctx, activation))
		require.NoError(t, logic.ProcessTask(ctx, activation))
		require.NoError(t, inspector.DeleteTask("default", job.ID))
		f.request(http.MethodPost, redeem, customer, body, xerr.InvalidParams)
		sub := f.client.UserSubscribe.Query().OnlyX(ctx)
		require.Equal(t, invoice.ID, sub.OrderID)
		require.Equal(t, owner.ID, sub.UserID)
		require.Equal(t, plan.ID, sub.SubscribeID)
		require.EqualValues(t, 1024, sub.Traffic)
		require.Zero(t, sub.Download)
		require.Zero(t, sub.Upload)
		if index == 0 {
			subscriptionID = sub.ID
			require.True(t, sub.StartTime.AddDate(0, 0, 2).Equal(sub.ExpireTime))
			f.client.UserSubscribe.UpdateOneID(sub.ID).SetDownload(400).SetUpload(100).SaveX(ctx)
		} else {
			require.Equal(t, subscriptionID, sub.ID)
			require.True(t, previousExpiry.AddDate(0, 0, 2).Equal(sub.ExpireTime))
		}
		previousExpiry = sub.ExpireTime
		require.EqualValues(t, 1, f.client.RedemptionCode.GetX(ctx, code.Id).UsedCount)
		require.EqualValues(t, order.StatusFinished, f.client.Order.GetX(ctx, invoice.ID).Status)
		f.request(http.MethodPut, adminPath+"/code", admin, fmt.Sprintf(`{"id":"%d","total_count":1,"quantity":1,"unit_time":"month"}`, code.Id), http.StatusOK)
		_, newCustomer := f.account(false)
		f.request(http.MethodPost, redeem, newCustomer, body, xerr.InvalidParams)
		f.request(http.MethodDelete, adminPath+"/code", admin, fmt.Sprintf(`{"id":"%d"}`, code.Id), http.StatusOK)
		f.request(http.MethodPost, redeem, newCustomer, body, xerr.InvalidParams)
		var history types.GetRedemptionRecordListResponse
		path := fmt.Sprintf("%s/record/list?page=1&size=10&code_id=%d&user_id=%d", adminPath, code.Id, owner.ID)
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, admin, "", http.StatusOK), &history))
		require.Len(t, history.List, 1)
		require.Equal(t, code.Id, history.List[0].RedemptionCodeId)
		require.Equal(t, owner.ID, history.List[0].UserId)
		require.Equal(t, "Day", history.List[0].UnitTime)
		require.EqualValues(t, 2, history.List[0].Quantity)
	}
	require.Equal(t, 2, f.client.Order.Query().CountX(ctx))
}
