package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/perfect-panel/server/ent"
	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/ticket"
	"github.com/stretchr/testify/require"
)

type supportBot struct {
	logic   *TelegramLogic
	db      *ent.Client
	replies []string
	chatID  int64
}

func newSupportBot(t *testing.T) *supportBot {
	t.Helper()
	logic, db, _, _ := bindingFixture(t)
	b := &supportBot{logic: logic, db: db, chatID: 230}
	db.User.UpdateOneID(7).SetIsAdmin(true).ExecX(t.Context())
	db.UserAuthMethod.Create().SetUserID(7).SetAuthType("telegram").SetAuthIdentifier(strconv.FormatInt(b.chatID, 10)).SetVerified(true).SaveX(t.Context())
	client, err := tgbot.New("123456:fixture", tgbot.WithSkipGetMe(), tgbot.WithHTTPClient(time.Second, botRequestFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/sendMessage") {
			return nil, fmt.Errorf("unexpected bot request %s %s", req.Method, req.URL.Path)
		}
		if err := req.ParseMultipartForm(1 << 20); err != nil {
			return nil, err
		}
		defer req.MultipartForm.RemoveAll()
		if req.FormValue("chat_id") != strconv.FormatInt(b.chatID, 10) {
			return nil, fmt.Errorf("unexpected reply recipient")
		}
		b.replies = append(b.replies, req.FormValue("text"))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":1}}`))}, nil
	})))
	require.NoError(t, err)
	logic.svcCtx.TelegramBot = client
	return b
}

func (b *supportBot) send(t *testing.T, text string) string {
	t.Helper()
	cmd, _, _ := strings.Cut(text, " ")
	message := commandMessage(text, len(cmd))
	message.Chat = models.Chat{ID: b.chatID, Type: models.ChatTypePrivate}
	message.From = &models.User{ID: b.chatID}
	before := len(b.replies)
	b.logic.TelegramLogic(&models.Update{Message: message})
	require.Len(t, b.replies, before+1)
	return b.replies[before]
}

func confirmationCommand(t *testing.T, reply, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(reply, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("missing %s in confirmation %q", prefix, reply)
	return ""
}

func TestSupportBotRepliesAndClosesTicketOnlyAfterConfirmation(t *testing.T) {
	b := newSupportBot(t)
	ctx := t.Context()
	owner := b.db.User.Create().SetPassword("fixture").SaveX(ctx)
	open := b.db.Ticket.Create().SetUserID(owner.ID).SetTitle("Replacement invoice").SetDescription("Please include the billing address").SetStatus(ticket.Pending).SaveX(ctx)
	b.db.Ticket.Create().SetUserID(owner.ID).SetTitle("Old invoice").SetStatus(ticket.Closed).SaveX(ctx)
	require.Contains(t, b.send(t, "/dash"), "Pending tickets: 1")
	list := b.send(t, "/tickets_waiting")
	require.Contains(t, list, open.Title)
	require.NotContains(t, list, "Old invoice")
	require.Equal(t, "Reply saved.", b.send(t, fmt.Sprintf("/rp_%d Invoice is ready", open.ID)))
	require.EqualValues(t, ticket.Waiting, b.db.Ticket.GetX(ctx, open.ID).Status)
	follow := b.db.TicketFollow.Query().OnlyX(ctx)
	require.Equal(t, open.ID, follow.TicketID)
	require.Equal(t, "Invoice is ready", follow.Content)
	detail := b.send(t, fmt.Sprintf("/tk_%d", open.ID))
	require.Contains(t, detail, open.Description)
	require.Contains(t, detail, "admin: Invoice is ready")
	req := fmt.Sprintf("/close_%d", open.ID)
	first := b.send(t, req)
	require.EqualValues(t, ticket.Waiting, b.db.Ticket.GetX(ctx, open.ID).Status)
	require.Equal(t, "Confirmation cancelled.", b.send(t, confirmationCommand(t, first, "/cancel_")))
	require.Equal(t, "Confirmation expired or unavailable.", b.send(t, confirmationCommand(t, first, "/confirm_")))
	require.EqualValues(t, ticket.Waiting, b.db.Ticket.GetX(ctx, open.ID).Status)
	confirm := confirmationCommand(t, b.send(t, req), "/confirm_")
	require.Equal(t, "Operation completed.", b.send(t, confirm))
	require.EqualValues(t, ticket.Closed, b.db.Ticket.GetX(ctx, open.ID).Status)
	require.Equal(t, "Confirmation expired or unavailable.", b.send(t, confirm))
	require.Contains(t, b.send(t, fmt.Sprintf("/rp %d Not delivered", open.ID)), "closed")
	require.Equal(t, 1, b.db.TicketFollow.Query().CountX(ctx))
	require.Equal(t, "Ticket reopened.", b.send(t, fmt.Sprintf("/reopen %d", open.ID)))
	require.Contains(t, b.send(t, "/tickets_waiting"), open.Title)
	rows := b.db.SystemLog.Query().AllX(ctx)
	var actions []string
	for _, row := range rows {
		if row.Type != modellog.TypeAdminAction.Uint8() {
			continue
		}
		var entry modellog.AdminAction
		require.NoError(t, json.Unmarshal([]byte(row.Content), &entry))
		require.Equal(t, "telegram", entry.Source)
		require.EqualValues(t, 7, entry.ActorID)
		require.Equal(t, b.chatID, entry.TelegramSenderID)
		require.Equal(t, open.ID, entry.ObjectID)
		actions = append(actions, entry.Action)
	}
	require.ElementsMatch(t, []string{"ticket.reply", "ticket.status", "ticket.status"}, actions)
}

func TestSupportBotCanResetAndResumeACustomerSubscription(t *testing.T) {
	b := newSupportBot(t)
	ctx := t.Context()
	owner := b.db.User.Create().SetPassword("fixture").SaveX(ctx)
	b.db.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier("member@example.test").SetVerified(true).SaveX(ctx)
	plan := b.db.Subscribe.Create().SetName("Regional").SaveX(ctx)
	expires := time.Now().Add(24 * time.Hour)
	sub := b.db.UserSubscribe.Create().SetUserID(owner.ID).SetOrderID(1).SetSubscribeID(plan.ID).SetExpireTime(expires).SetStatus(1).SetUpload(17).SetDownload(52).SaveX(ctx)
	other := b.db.UserSubscribe.Create().SetUserID(7).SetOrderID(2).SetSubscribeID(plan.ID).SetExpireTime(expires).SetStatus(1).SetUpload(3).SetDownload(8).SaveX(ctx)
	require.Contains(t, b.send(t, "/user_sub member@example.test"), fmt.Sprintf("Subscription #%d: status 1; upload 17; download 52", sub.ID))
	apply := func(verb string) {
		t.Helper()
		prompt := b.send(t, fmt.Sprintf("/%s %d", verb, sub.ID))
		require.Equal(t, "Operation completed.", b.send(t, confirmationCommand(t, prompt, "/confirm_")))
	}
	apply("reset")
	updated := b.db.UserSubscribe.GetX(ctx, sub.ID)
	require.Zero(t, updated.Upload)
	require.Zero(t, updated.Download)
	require.EqualValues(t, 1, updated.Status)
	apply("toggle")
	require.EqualValues(t, 5, b.db.UserSubscribe.GetX(ctx, sub.ID).Status)
	apply("toggle")
	require.EqualValues(t, 1, b.db.UserSubscribe.GetX(ctx, sub.ID).Status)
	unchanged := b.db.UserSubscribe.GetX(ctx, other.ID)
	require.Equal(t, other.Upload, unchanged.Upload)
	require.Equal(t, other.Download, unchanged.Download)
}
