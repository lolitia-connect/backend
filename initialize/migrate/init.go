package migrate

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
)

// CreateAdminUser create admin user
func CreateAdminUser(ctx context.Context, email, password string, client *ent.Client) error {
	enable := true
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := func() error {
		// Prevent duplicate creation
		count, err := tx.Client().User.Query().Count(ctx)
		if err != nil {
			return err
		}
		if count != 0 {
			logger.Info("User already exists, skip creating administrator account")
			return nil
		}

		u := user.User{
			Password:  tool.EncodePassWord(password),
			IsAdmin:   &enable,
			ReferCode: uuidx.UserInviteCode(time.Now().Unix()),
		}
		created, err := tx.Client().User.Create().SetPassword(u.Password).SetIsAdmin(enable).SetReferCode(u.ReferCode).Save(ctx)
		if err != nil {
			return err
		}
		method := user.AuthMethods{
			UserId:         created.ID,
			AuthType:       "email",
			AuthIdentifier: email,
			Verified:       true,
		}
		if _, err := tx.Client().UserAuthMethod.Create().SetUserID(method.UserId).SetAuthType(method.AuthType).SetAuthIdentifier(method.AuthIdentifier).SetVerified(method.Verified).Save(ctx); err != nil {
			return err
		}
		return nil
	}(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
