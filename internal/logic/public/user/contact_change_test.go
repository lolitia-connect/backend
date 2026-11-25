package user

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/middleware"
	usermodel "github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/internal/usersession"
	"github.com/perfect-panel/server/pkg/constant"
)

func TestChangingEmailRequiresBothProofsAndRevokesSession(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	service.Config.JwtAuth.AccessSecret = "account-fixture-secret"
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	const oldEmail, newEmail = "old@example.test", "new@example.test"
	credential := client.UserAuthMethod.Create().SetUserID(owner.ID).SetAuthType("email").SetAuthIdentifier(oldEmail).SetVerified(true).SaveX(ctx)
	account, err := service.Store.User().FindOne(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "contact-change-session"
	token, err := usersession.Issue(ctx, service.Redis, service.Config.JwtAuth.AccessSecret, 300, owner.ID, sessionID, "email", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Redis.Set(ctx, fmt.Sprintf("%s:%s", config.SessionIdKey, sessionID), owner.ID, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	const route = "/v1/public/user/info"
	if _, err := middleware.AuthenticateRequest(ctx, service, token, route); err != nil {
		t.Fatalf("original contact session did not authenticate: %v", err)
	}
	proof := func(kind constant.VerifyType, email, code string) string {
		t.Helper()
		key := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, kind, email)
		if err := service.Redis.Set(ctx, key, fmt.Sprintf(`{"code":%q}`, code), time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		return key
	}
	newKey := proof(constant.Register, newEmail, "123456")
	logic := NewUpdateBindEmailLogic(context.WithValue(ctx, constant.CtxKeyUser, account), service)
	request := &types.UpdateBindEmailRequest{Email: " New@Example.Test ", Code: "123456"}
	if err := logic.UpdateBindEmail(request); err == nil || client.UserAuthMethod.GetX(ctx, credential.ID).AuthIdentifier != oldEmail {
		t.Fatal("proof of the new email alone changed the login")
	}
	oldKey := proof(constant.Security, oldEmail, "654321")
	request.CurrentCode = "654321"
	if err := logic.UpdateBindEmail(request); err != nil {
		t.Fatal(err)
	}
	updated := client.UserAuthMethod.GetX(ctx, credential.ID)
	if updated.UserID != owner.ID || updated.AuthIdentifier != newEmail || !updated.Verified {
		t.Fatal("existing credential was not replaced with the verified canonical email")
	}
	if count := service.Redis.Exists(ctx, oldKey, newKey).Val(); count != 0 {
		t.Fatalf("%d verification codes remain reusable after the change", count)
	}
	if _, err := middleware.AuthenticateRequest(ctx, service, token, route); err == nil {
		t.Fatal("email change left the previous login session active")
	}
}

func TestMobileBindingRejectsNumberAssignedToAnotherAccount(t *testing.T) {
	service, client, _ := newAccountFixture(t)
	ctx := t.Context()
	owner := client.User.Create().SetPassword("fixture").SaveX(ctx)
	other := client.User.Create().SetPassword("fixture").SaveX(ctx)
	const owned, unclaimed = "+12025550123", "+12025550124"
	otherCredential := client.UserAuthMethod.Create().SetUserID(other.ID).SetAuthType("mobile").SetAuthIdentifier(owned).SaveX(ctx)
	setProof := func(number string) {
		t.Helper()
		key := fmt.Sprintf("%s:%s:%s", config.AuthCodeTelephoneCacheKey, constant.Register, number)
		if err := service.Redis.Set(ctx, key, `{"code":"123456"}`, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	logic := NewUpdateBindMobileLogic(context.WithValue(ctx, constant.CtxKeyUser, &usermodel.User{Id: owner.ID}), service)
	request := &types.UpdateBindMobileRequest{AreaCode: "1", Mobile: "2025550123", Code: "123456"}
	setProof(owned)
	if err := logic.UpdateBindMobile(request); err == nil || client.UserAuthMethod.Query().CountX(ctx) != 1 {
		t.Fatal("binding proof was allowed to claim a number already assigned to another user")
	}
	request.Mobile = "2025550124"
	setProof(unclaimed)
	if err := logic.UpdateBindMobile(request); err != nil {
		t.Fatal(err)
	}
	current, err := service.Store.User().FindUserAuthMethodByUserId(ctx, "mobile", owner.ID)
	if err != nil || current.AuthIdentifier != unclaimed || !current.Verified {
		t.Fatalf("new mobile binding = %+v, %v", current, err)
	}
	if stored := client.UserAuthMethod.GetX(ctx, otherCredential.ID); stored.UserID != other.ID || stored.AuthIdentifier != owned {
		t.Fatal("other account's mobile number was changed")
	}
}
