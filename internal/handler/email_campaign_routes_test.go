package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/types"
	emailworker "github.com/perfect-panel/server/queue/logic/email"
	queue "github.com/perfect-panel/server/queue/types"
	"github.com/stretchr/testify/require"
)

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
