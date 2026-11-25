package oauth

import (
	"testing"
)

func TestOAuthExistingIdentityAndInvitationPolicy(t *testing.T) {
	service, client, _, ctx := newOAuthTestService(t)
	logic := NewOAuthLoginGetTokenLogic(ctx, service)
	account := client.User.Create().SetPassword("fixture").SetEnable(true).SaveX(ctx)
	client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType(OAuthGoogle).SetAuthIdentifier("existing-google").SetVerified(true).SaveX(ctx)
	got, err := logic.findOrRegisterUser(OAuthGoogle, "existing-google", "", "", "", true, "existing", "192.0.2.4", "fixture-agent")
	if err != nil || got == nil || got.Id != account.ID {
		t.Fatalf("existing OAuth identity = %+v, %v", got, err)
	}
	client.UserAuthMethod.Create().SetUserID(account.ID).SetAuthType(OAuthFacebook).SetAuthIdentifier("unverified-facebook").SetVerified(false).SaveX(ctx)
	if _, err := logic.findOrRegisterUser(OAuthFacebook, "unverified-facebook", "", "", "", true, "unverified", "192.0.2.4", "fixture-agent"); err == nil {
		t.Fatal("unverified OAuth identity was accepted")
	}
	service.Config.Invite.ForcedInvite = true
	_, err = logic.findOrRegisterUser(OAuthTelegram, "pending-invite", "person@example.test", "", "", true, "invite", "192.0.2.4", "fixture-agent")
	pending, ok := err.(*oauthRegistrationPending)
	if !ok || !pending.inviteRequired || pending.emailRequired {
		t.Fatalf("forced invitation result = %+v, %v", pending, err)
	}
	if client.User.Query().Where().CountX(ctx) != 1 {
		t.Fatal("pending invitation created a user")
	}
	if _, err := logic.findOrRegisterUser(OAuthTelegram, "bad-invite", "person@example.test", "", "missing", true, "bad-invite", "192.0.2.4", "fixture-agent"); err == nil {
		t.Fatal("invalid invitation was accepted")
	}
}
