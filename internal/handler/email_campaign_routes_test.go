package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	emailworker "github.com/perfect-panel/server/queue/logic/email"
	quotaworker "github.com/perfect-panel/server/queue/logic/task"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

func TestQuotaCampaignCreditsOnlyItsSavedAudienceAfterRetry(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	owner, _ := f.account(false)
	wallet := f.client.UserWallet.Create().SetUserID(owner.ID).SetGiftAmount(25).SaveX(ctx)
	plan := f.client.Subscribe.Create().SetName("Travel").SetUnitPrice(1250).SaveX(ctx)
	inspector := f.startQueue()
	expires := time.Now().UTC().Truncate(time.Second).AddDate(0, 0, 10)
	newSubscription := func(orderID int64) *ent.UserSubscribe {
		t.Helper()
		return f.client.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(orderID).SetSubscribeID(plan.ID).
			SetStatus(1).SetExpireTime(expires).SetDownload(700).SetToken(fmt.Sprintf("campaign-%d", orderID)).SaveX(ctx)
	}
	first, second := newSubscription(1), newSubscription(2)
	const editor = "/v1/admin/marketing/quota"
	form := fmt.Sprintf(`{"subscribers":["%d"],"is_active":true,"days":2,"gift_type":2,"gift_value":12,"reset_traffic":true}`, plan.ID)
	invalid := strings.Replace(form, `"]`, `","no-such-plan"]`, 1)
	for _, path := range []string{"/pre-count", "/create"} {
		require.Empty(t, f.request(http.MethodPost, editor+path, operator, invalid, xerr.InvalidParams))
	}
	require.Zero(t, f.client.Task.Query().CountX(ctx))
	var preview types.QueryQuotaTaskPreCountResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, editor+"/pre-count", operator, form, http.StatusOK), &preview))
	require.EqualValues(t, 2, preview.Count)
	f.request(http.MethodPost, editor+"/create", operator, form, http.StatusOK)
	late := newSubscription(3)
	pending, err := inspector.ListPendingTasks("default")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, queue.ForthwithQuotaTask, pending[0].Type)
	job := asynq.NewTask(pending[0].Type, pending[0].Payload)
	worker := quotaworker.NewQuotaTaskLogic(f.service)
	history := func(status int8, current int64) {
		t.Helper()
		var page types.QueryQuotaTaskListResponse
		path := fmt.Sprintf("%s/list?status=%d&page=1&size=5", editor, status)
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, operator, "", http.StatusOK), &page))
		require.EqualValues(t, 1, page.Total)
		require.Len(t, page.List, 1)
		item := page.List[0]
		require.Equal(t, fmt.Sprint(item.Id), string(job.Payload()))
		require.Equal(t, current, item.Current)
		require.Equal(t, preview.Count, item.Total)
		require.Empty(t, item.Errors)
		require.ElementsMatch(t, []string{fmt.Sprint(first.ID), fmt.Sprint(second.ID)}, item.Objects)
	}
	credits := 0
	f.client.UserWallet.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			credits++
			if credits == 2 {
				return nil, errors.New("credit posting interrupted")
			}
			return next.Mutate(ctx, m)
		})
	})
	require.ErrorContains(t, worker.ProcessTask(ctx, job), "credit posting interrupted")
	history(task.StatusPending, 0)
	require.Equal(t, wallet.GiftAmount, f.client.UserWallet.GetX(ctx, wallet.ID).GiftAmount)
	require.Zero(t, f.client.SystemLog.Query().CountX(ctx))
	for _, sub := range []*ent.UserSubscribe{first, second} {
		stored := f.client.UserSubscribe.GetX(ctx, sub.ID)
		require.Equal(t, sub.Download, stored.Download)
		require.True(t, expires.Equal(stored.ExpireTime))
	}
	require.NoError(t, worker.ProcessTask(ctx, job))
	require.NoError(t, worker.ProcessTask(ctx, job))
	history(task.StatusCompleted, 2)
	require.EqualValues(t, 325, f.client.UserWallet.GetX(ctx, wallet.ID).GiftAmount)
	for _, sub := range []*ent.UserSubscribe{first, second} {
		stored := f.client.UserSubscribe.GetX(ctx, sub.ID)
		require.Zero(t, stored.Download)
		require.True(t, expires.AddDate(0, 0, 2).Equal(stored.ExpireTime))
		var resets types.FilterResetSubscribeLogResponse
		path := fmt.Sprintf("/v1/admin/log/subscribe/reset/list?user_subscribe_id=%d", sub.ID)
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, operator, "", http.StatusOK), &resets))
		require.EqualValues(t, 1, resets.Total)
		require.Len(t, resets.List, 1)
		require.Equal(t, owner.ID, resets.List[0].UserId)
		require.Equal(t, sub.ID, resets.List[0].UserSubscribeId)
	}
	var gifts types.FilterGiftLogResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, fmt.Sprintf("/v1/admin/log/gift/list?user_id=%d", owner.ID), operator, "", http.StatusOK), &gifts))
	require.EqualValues(t, 2, gifts.Total)
	require.Len(t, gifts.List, 2)
	var recipients []int64
	for _, gift := range gifts.List {
		require.EqualValues(t, 150, gift.Amount)
		recipients = append(recipients, gift.SubscribeId)
	}
	require.ElementsMatch(t, []int64{first.ID, second.ID}, recipients)
	unchanged := f.client.UserSubscribe.GetX(ctx, late.ID)
	require.True(t, expires.Equal(unchanged.ExpireTime))
	require.Equal(t, late.Download, unchanged.Download)
}

func TestDailyCampaignResumesWithTheNextRecipient(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	inbox := captureMail(t, f)
	inspector := f.startQueue()
	_, operator := f.account(true)
	const editor = "/v1/admin/marketing/email/batch"
	const subject = "April maintenance"
	f.request(http.MethodPost, editor+"/send", operator, `{"subject":"April maintenance","content":"The panel reopens on Tuesday","scope":5,"limit":1,"additional":"one@example.test\ntwo@example.test\none@example.test"}`, http.StatusOK)
	initial, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	require.Len(t, initial, 1)
	current := f.client.Task.Query().OnlyX(ctx)
	status := func(want int8, sent int64) {
		t.Helper()
		var report types.GetBatchSendEmailTaskStatusResponse
		data := f.request(http.MethodPost, editor+"/status", operator, fmt.Sprintf(`{"id":"%d"}`, current.ID), http.StatusOK)
		require.NoError(t, json.Unmarshal(data, &report))
		require.EqualValues(t, want, report.Status)
		require.EqualValues(t, 2, report.Total)
		require.Equal(t, sent, report.Current)
		require.Empty(t, report.Errors)
	}
	status(task.StatusPending, 0)
	worker := emailworker.NewBatchEmailLogic(f.service)
	first := asynq.NewTask(initial[0].Type, initial[0].Payload)
	require.NoError(t, inspector.DeleteTask("default", initial[0].ID))
	require.NoError(t, worker.ProcessTask(ctx, first))
	require.NoError(t, worker.ProcessTask(ctx, first))
	status(task.StatusInProgress, 1)
	retry, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	require.Len(t, retry, 1)
	require.Equal(t, initial[0].Type, retry[0].Type)
	require.Equal(t, initial[0].Payload, retry[0].Payload)
	checkpoint := f.client.Task.GetX(ctx, current.ID)
	day, err := time.ParseInLocation(time.DateOnly, checkpoint.DailyDate, time.Local)
	require.NoError(t, err)
	require.Equal(t, day.AddDate(0, 0, 1).Unix(), retry[0].NextProcessAt.Unix())
	// Load the persisted cursor as a worker would on the next billing day.
	f.client.Task.UpdateOneID(current.ID).SetDailyDate(day.AddDate(0, 0, -1).Format(time.DateOnly)).ExecX(ctx)
	continuation := asynq.NewTask(retry[0].Type, retry[0].Payload)
	require.NoError(t, inspector.DeleteTask("default", retry[0].ID))
	require.NoError(t, worker.ProcessTask(ctx, continuation))
	require.NoError(t, worker.ProcessTask(ctx, continuation))
	status(task.StatusCompleted, 2)
	deliveries := inbox()
	require.Len(t, deliveries, 2)
	var recipients []string
	for _, delivery := range deliveries {
		recipients = append(recipients, delivery.to)
		require.Contains(t, string(delivery.message), subject)
		require.Contains(t, string(delivery.message), "The panel reopens on Tuesday")
	}
	require.ElementsMatch(t, []string{"<one@example.test>", "<two@example.test>"}, recipients)
}

func TestOperatorFiltersCampaignHistoryBeforePagination(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	created := time.Date(2025, 4, 2, 9, 0, 0, 0, time.UTC)
	seed := func(subject, scope string, kind, status int8) int64 {
		t.Helper()
		created = created.Add(time.Hour)
		content, err := json.Marshal(task.EmailContent{Subject: subject, Content: "Maintenance window"})
		require.NoError(t, err)
		return f.client.Task.Create().SetType(kind).SetScope(scope).SetContent(string(content)).SetStatus(status).SetCreatedAt(created).SaveX(ctx).ID
	}
	const subscribers = `{"Type":2,"recipients":["member@example.test"]}`
	first := seed("April maintenance", subscribers, task.TypeEmail, task.StatusCancelled)
	seed("Notice to all members", `{"Type":1}`, task.TypeEmail, task.StatusCancelled)
	latest := seed("Revised window", subscribers, task.TypeEmail, task.StatusCancelled)
	seed("Upcoming window", subscribers, task.TypeEmail, task.StatusPending)
	seed("Legacy record", "invalid scope", task.TypeEmail, task.StatusCancelled)
	seed("Traffic adjustment", subscribers, task.TypeQuota, task.StatusCancelled)
	for page, want := range [][]int64{{latest}, {first}, {}} {
		var history types.GetBatchSendEmailTaskListResponse
		path := fmt.Sprintf("/v1/admin/marketing/email/batch/list?scope=2&status=%d&page=%d&size=1", task.StatusCancelled, page+1)
		require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path, operator, "", http.StatusOK), &history))
		require.EqualValues(t, 2, history.Total)
		require.Len(t, history.List, len(want))
		for i, id := range want {
			require.Equal(t, id, history.List[i].Id)
			require.Equal(t, "member@example.test", history.List[i].Recipients)
		}
	}
}

func TestOperatorCanPreviewScheduleAndCancelSubscriberEmail(t *testing.T) {
	f := newRouteFixture(t)
	ctx := t.Context()
	_, operator := f.account(true)
	inspector := f.startQueue()
	plan := f.client.Subscribe.Create().SetName("Monthly").SaveX(ctx)
	for i, recipient := range []struct {
		address string
		status  uint8
	}{{"active@example.test", 1}, {"expired@example.test", 3}} {
		account := f.client.User.Create().SetPassword("fixture").SaveX(ctx)
		f.client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType("email").SetAuthIdentifier(recipient.address).SaveX(ctx)
		f.client.UserSubscribe.Create().SetUserID(account.ID).SetSubscribeID(plan.ID).SetOrderID(int64(i + 1)).
			SetStatus(recipient.status).SetToken(fmt.Sprintf("subscriber-%d", i)).SaveX(ctx)
	}
	const path = "/v1/admin/marketing/email/batch"
	var preview types.GetPreSendEmailCountResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path+"/pre-send-count", operator, `{"scope":2}`, http.StatusOK), &preview))
	require.EqualValues(t, 1, preview.Count)
	scheduled := time.Now().Add(time.Hour).Truncate(time.Second)
	payload := fmt.Sprintf(`{"subject":"Plan update","content":"Service window","scope":2,"additional":"active@example.test\nextra@example.test\nextra@example.test","scheduled":%d}`, scheduled.Unix())
	f.request(http.MethodPost, path+"/send", operator, payload, http.StatusOK)
	var campaigns types.GetBatchSendEmailTaskListResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodGet, path+"/list", operator, "", http.StatusOK), &campaigns))
	require.EqualValues(t, 1, campaigns.Total)
	require.Len(t, campaigns.List, 1)
	campaign := campaigns.List[0]
	require.Equal(t, "Plan update", campaign.Subject)
	require.Equal(t, "active@example.test", campaign.Recipients)
	require.ElementsMatch(t, []string{"active@example.test", "extra@example.test"}, strings.Split(campaign.Additional, "\n"))
	require.EqualValues(t, 2, campaign.Total)
	tasks, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, queue.ScheduledBatchSendEmail, tasks[0].Type)
	require.Equal(t, strconv.FormatInt(campaign.Id, 10), string(tasks[0].Payload))
	require.Equal(t, scheduled.Unix(), tasks[0].NextProcessAt.Unix())

	id := fmt.Sprintf(`{"id":"%d"}`, campaign.Id)
	f.request(http.MethodPost, path+"/stop", operator, id, http.StatusOK)
	require.NoError(t, emailworker.NewBatchEmailLogic(f.service).ProcessTask(ctx, asynq.NewTask(tasks[0].Type, tasks[0].Payload)))
	var status types.GetBatchSendEmailTaskStatusResponse
	require.NoError(t, json.Unmarshal(f.request(http.MethodPost, path+"/status", operator, id, http.StatusOK), &status))
	require.EqualValues(t, task.StatusCancelled, status.Status)
	require.EqualValues(t, 2, status.Total)
	require.Zero(t, status.Current)
}
