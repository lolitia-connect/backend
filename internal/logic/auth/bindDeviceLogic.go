package auth

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type BindDeviceLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBindDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BindDeviceLogic {
	return &BindDeviceLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// BindDeviceToUser binds a device to a user
// If the device is already bound to another user, it will disable that user and bind the device to the current user
func (l *BindDeviceLogic) BindDeviceToUser(identifier, ip, userAgent string, currentUserId int64) error {
	if identifier == "" {
		// No device identifier provided, skip binding
		return nil
	}

	l.Infow("binding device to user",
		logger.Field("identifier", identifier),
		logger.Field("user_id", currentUserId),
		logger.Field("ip", ip),
	)

	// Check if device exists
	deviceInfo, err := l.svcCtx.Store.User().FindOneDeviceByIdentifier(l.ctx, identifier)
	if err != nil {
		if ent.IsNotFound(err) {
			// Device not found, create new device record
			return l.createDeviceForUser(identifier, ip, userAgent, currentUserId)
		}
		l.Errorw("failed to query device",
			logger.Field("identifier", identifier),
			logger.Field("error", err.Error()),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query device failed: %v", err.Error())
	}

	// Device exists, check if it's bound to current user
	if deviceInfo.UserId == currentUserId {
		// Already bound to current user, just update IP and UserAgent
		l.Infow("device already bound to current user, updating info",
			logger.Field("identifier", identifier),
			logger.Field("user_id", currentUserId),
		)
		deviceInfo.Ip = ip
		deviceInfo.UserAgent = userAgent
		if err := l.svcCtx.Store.User().UpdateDevice(l.ctx, deviceInfo); err != nil {
			l.Errorw("failed to update device",
				logger.Field("identifier", identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err.Error())
		}
		return nil
	}

	// Device is bound to another user, need to disable old user and rebind
	l.Infow("device bound to another user, rebinding",
		logger.Field("identifier", identifier),
		logger.Field("old_user_id", deviceInfo.UserId),
		logger.Field("new_user_id", currentUserId),
	)

	return l.rebindDeviceToNewUser(deviceInfo, ip, userAgent, currentUserId)
}

func (l *BindDeviceLogic) createDeviceForUser(identifier, ip, userAgent string, userId int64) error {
	l.Infow("creating new device for user",
		logger.Field("identifier", identifier),
		logger.Field("user_id", userId),
	)

	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// Create device auth method
		authMethod := &user.AuthMethods{
			UserId:         userId,
			AuthType:       "device",
			AuthIdentifier: identifier,
			Verified:       true,
		}
		if err := store.User().InsertUserAuthMethods(l.ctx, authMethod); err != nil {
			if ent.IsConstraintError(err) {
				// Concurrent request already created this auth method;
				// propagate so the outer handler can retry gracefully.
				return err
			}
			l.Errorw("failed to create device auth method",
				logger.Field("user_id", userId),
				logger.Field("identifier", identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create device auth method failed: %v", err)
		}

		// Create device record
		deviceInfo := &user.Device{
			Ip:     ip,
			UserId: userId,

			UserAgent:  userAgent,
			Identifier: identifier,
			Enabled:    true,
			Online:     false,
		}
		if err := store.User().InsertDevice(l.ctx, deviceInfo); err != nil {
			l.Errorw("failed to create device",
				logger.Field("user_id", userId),
				logger.Field("identifier", identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create device failed: %v", err)
		}

		return nil
	})

	// Handle duplicate key from concurrent device creation.
	// The transaction is rolled back, and another request has already committed
	// the device record — re-read it and route to the appropriate path.
	if ent.IsConstraintError(err) {
		l.Infow("device concurrently created, retrying as existing device",
			logger.Field("identifier", identifier),
			logger.Field("user_id", userId),
		)

		deviceInfo, findErr := l.svcCtx.Store.User().FindOneDeviceByIdentifier(l.ctx, identifier)
		if findErr != nil {
			l.Errorw("failed to find device after concurrent creation",
				logger.Field("identifier", identifier),
				logger.Field("error", findErr.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find device after concurrent creation failed: %v", findErr)
		}

		// Already bound to current user — just update IP / UserAgent
		if deviceInfo.UserId == userId {
			deviceInfo.Ip = ip
			deviceInfo.UserAgent = userAgent
			if err := l.svcCtx.Store.User().UpdateDevice(l.ctx, deviceInfo); err != nil {
				l.Errorw("failed to update device",
					logger.Field("identifier", identifier),
					logger.Field("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err)
			}
			return nil
		}

		// Bound to another user — rebind
		return l.rebindDeviceToNewUser(deviceInfo, ip, userAgent, userId)
	}

	if err != nil {
		l.Errorw("device creation failed",
			logger.Field("identifier", identifier),
			logger.Field("user_id", userId),
			logger.Field("error", err.Error()),
		)
		return err
	}

	l.Infow("device created successfully",
		logger.Field("identifier", identifier),
		logger.Field("user_id", userId),
	)

	return nil
}

func (l *BindDeviceLogic) rebindDeviceToNewUser(deviceInfo *user.Device, ip, userAgent string, newUserId int64) error {
	oldUserId := deviceInfo.UserId

	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// Check if old user has other auth methods besides device
		authMethods, err := store.User().FindUserAuthMethods(l.ctx, oldUserId)
		if err != nil {
			l.Errorw("failed to query auth methods for old user",
				logger.Field("old_user_id", oldUserId),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query auth methods failed: %v", err)
		}

		//如果没有其他认证方式，禁用旧用户账号
		nonDeviceAuthCount := 0
		for _, am := range authMethods {
			if am.AuthType != "device" {
				nonDeviceAuthCount++
			}
		}
		if nonDeviceAuthCount == 0 {
			//检查设备下是否有套餐，有套餐。就检查即将绑定过去的所有账户是否有套餐，如果有，那么检查两个套餐是否一致。如果一致就将即将删除的用户套餐，时间叠加到我绑定过去的用户套餐上面（如果套餐已过期就忽略）。新绑定设备的账户上套餐不一致或者不存在直接将套餐换绑即可
			_, err = store.Ent().UserSubscribe.Query().Where(
				usersubscribe.UserID(oldUserId),
				usersubscribe.StatusIn(0, 1),
			).All(l.ctx)
			if err != nil {
				l.Errorw("failed to query old user subscribes",
					logger.Field("old_user_id", oldUserId),
					logger.Field("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query old user subscribes failed: %v", err)
			}
		}

		// Only disable old user if they have no other auth methods
		if nonDeviceAuthCount == 0 {
			falseVal := false
			oldUser, err := store.User().FindOne(l.ctx, oldUserId)
			if err != nil {
				l.Errorw("failed to find old user",
					logger.Field("old_user_id", oldUserId),
					logger.Field("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find old user failed: %v", err)
			}
			oldUser.Enable = &falseVal
			if err := store.User().Update(l.ctx, oldUser); err != nil {
				l.Errorw("failed to disable old user",
					logger.Field("old_user_id", oldUserId),
					logger.Field("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "disable old user failed: %v", err)
			}
		}

		// Update device auth method to new user
		if err := store.User().UpdateUserAuthMethodOwner(l.ctx, "device", deviceInfo.Identifier, newUserId); err != nil {
			l.Errorw("failed to update device auth method",
				logger.Field("identifier", deviceInfo.Identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device auth method failed: %v", err)
		}

		// 更新设备绑定的用户id
		deviceInfo.UserId = newUserId
		deviceInfo.Ip = ip
		deviceInfo.UserAgent = userAgent
		deviceInfo.Enabled = true

		if err := store.User().UpdateDevice(l.ctx, deviceInfo); err != nil {
			l.Errorw("failed to update device",
				logger.Field("identifier", deviceInfo.Identifier),
				logger.Field("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err)
		}
		return nil
	})

	if err != nil {
		l.Errorw("device rebinding failed",
			logger.Field("identifier", deviceInfo.Identifier),
			logger.Field("old_user_id", oldUserId),
			logger.Field("new_user_id", newUserId),
			logger.Field("error", err.Error()),
		)
		return err
	}

	oldUser, err := l.svcCtx.Store.User().FindOne(l.ctx, oldUserId)
	if err != nil {
		l.Errorw("failed to find old user for cache clear",
			logger.Field("old_user_id", oldUserId),
			logger.Field("error", err.Error()),
		)
		return err
	}
	newUser, err := l.svcCtx.Store.User().FindOne(l.ctx, newUserId)
	if err != nil {
		l.Errorw("failed to find new user for cache clear",
			logger.Field("new_user_id", newUserId),
			logger.Field("error", err.Error()),
		)
		return err
	}
	err = l.svcCtx.Store.User().ClearUserCache(l.ctx, oldUser, newUser)
	if err != nil {
		l.Errorw("failed to clear user cache after rebinding",
			logger.Field("old_user_id", oldUserId),
			logger.Field("new_user_id", newUserId),
			logger.Field("error", err.Error()),
		)
	}

	l.Infow("device rebound successfully",
		logger.Field("identifier", deviceInfo.Identifier),
		logger.Field("old_user_id", oldUserId),
		logger.Field("new_user_id", newUserId),
	)

	return nil
}
