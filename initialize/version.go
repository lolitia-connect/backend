package initialize

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"

	"github.com/perfect-panel/server/initialize/migrate"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"go.uber.org/zap"
)

func Migrate(ctx *svc.ServiceContext) {
	mc := orm.Mysql{
		Config: ctx.Config.DatabaseConfig(),
	}
	now := time.Now()
	migration := migrate.Migrate(mc.Driver(), mc.MigrationDsn())
	defer migration.Close()
	if err := migration.Up(); err != nil {
		if errors.Is(err, migrate.NoChange) {
			zap.S().Info("[Migrate] database not change")
		} else {
			zap.S().Errorf("[Migrate] Up error: %v", err.Error())
			panic(err)
		}
	} else {
		zap.S().Info("[Migrate] Database change, took " + time.Since(now).String())
	}
	// if not found admin user
	generatedPassword := ""
	err := ctx.Store.InTx(context.Background(), func(store repository.Store) error {
		count, err := store.User().QueryResisterUserTotal(context.Background())
		if err != nil {
			return err
		}
		if count == 0 {
			password := ctx.Config.Administrator.Password
			if password == "" {
				generatedPassword = uuid.NewString()
				password = generatedPassword
			}
			enable := true
			admin := &user.User{
				Password:  tool.EncodePassWord(password),
				IsAdmin:   &enable,
				ReferCode: uuidx.UserInviteCode(time.Now().Unix()),
			}
			if err := store.User().Insert(context.Background(), admin); err != nil {
				zap.S().Errorf("[Migrate] CreateAdminUser error: %v", err.Error())
				return err
			}
			if err := store.User().InsertUserAuthMethods(context.Background(), &user.AuthMethods{
				UserId:         admin.Id,
				AuthType:       "email",
				AuthIdentifier: ctx.Config.Administrator.Email,
				Verified:       true,
			}); err != nil {
				zap.S().Errorf("[Migrate] CreateAdminUser error: %v", err.Error())
				return err
			}
			zap.S().Info("[Migrate] Create admin user success")
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	if generatedPassword != "" {
		zap.S().Warnf("Initial administrator %s password: %s", ctx.Config.Administrator.Email, generatedPassword)
	}
}
