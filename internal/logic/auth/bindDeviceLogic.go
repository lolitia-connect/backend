package auth

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BindDeviceLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewBindDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BindDeviceLogic {
	return &BindDeviceLogic{
		Logger: zap.S(),
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

	l.Logger.Infow("binding device to user",
		zap.Any("identifier", identifier),
		zap.Any("user_id", currentUserId),
		zap.Any("ip", ip),
	)

	// Check if device exists
	deviceInfo, err := l.svcCtx.Store.User().FindOneDeviceByIdentifier(l.ctx, identifier)
	if err != nil {
		if ent.IsNotFound(err) {
			// Device not found, create new device record
			return l.createDeviceForUser(identifier, ip, userAgent, currentUserId)
		}
		l.Logger.Errorw("failed to query device",
			zap.Any("identifier", identifier),
			zap.Any("error", err.Error()),
		)
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query device failed: %v", err.Error())
	}

	// Device exists, check if it's bound to current user
	if deviceInfo.UserId == currentUserId {
		// Already bound to current user, just update IP and UserAgent
		l.Logger.Infow("device already bound to current user, updating info",
			zap.Any("identifier", identifier),
			zap.Any("user_id", currentUserId),
		)
		deviceInfo.Ip = ip
		deviceInfo.UserAgent = userAgent
		if err := l.svcCtx.Store.User().UpdateDevice(l.ctx, deviceInfo); err != nil {
			l.Logger.Errorw("failed to update device",
				zap.Any("identifier", identifier),
				zap.Any("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err.Error())
		}
		return nil
	}

	// Device is bound to another user, need to disable old user and rebind
	l.Logger.Infow("device bound to another user, rebinding",
		zap.Any("identifier", identifier),
		zap.Any("old_user_id", deviceInfo.UserId),
		zap.Any("new_user_id", currentUserId),
	)

	return l.rebindDeviceToNewUser(deviceInfo, ip, userAgent, currentUserId)
}

func (l *BindDeviceLogic) createDeviceForUser(identifier, ip, userAgent string, userId int64) error {
	l.Logger.Infow("creating new device for user",
		zap.Any("identifier", identifier),
		zap.Any("user_id", userId),
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
			l.Logger.Errorw("failed to create device auth method",
				zap.Any("user_id", userId),
				zap.Any("identifier", identifier),
				zap.Any("error", err.Error()),
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
			l.Logger.Errorw("failed to create device",
				zap.Any("user_id", userId),
				zap.Any("identifier", identifier),
				zap.Any("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create device failed: %v", err)
		}

		return nil
	})

	// Handle duplicate key from concurrent device creation.
	// The transaction is rolled back, and another request has already committed
	// the device record — re-read it and route to the appropriate path.
	if ent.IsConstraintError(err) {
		l.Logger.Infow("device concurrently created, retrying as existing device",
			zap.Any("identifier", identifier),
			zap.Any("user_id", userId),
		)

		deviceInfo, findErr := l.svcCtx.Store.User().FindOneDeviceByIdentifier(l.ctx, identifier)
		if findErr != nil {
			l.Logger.Errorw("failed to find device after concurrent creation",
				zap.Any("identifier", identifier),
				zap.Any("error", findErr.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find device after concurrent creation failed: %v", findErr)
		}

		// Already bound to current user — just update IP / UserAgent
		if deviceInfo.UserId == userId {
			deviceInfo.Ip = ip
			deviceInfo.UserAgent = userAgent
			if err := l.svcCtx.Store.User().UpdateDevice(l.ctx, deviceInfo); err != nil {
				l.Logger.Errorw("failed to update device",
					zap.Any("identifier", identifier),
					zap.Any("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err)
			}
			return nil
		}

		// Bound to another user — rebind
		return l.rebindDeviceToNewUser(deviceInfo, ip, userAgent, userId)
	}

	if err != nil {
		l.Logger.Errorw("device creation failed",
			zap.Any("identifier", identifier),
			zap.Any("user_id", userId),
			zap.Any("error", err.Error()),
		)
		return err
	}

	l.Logger.Infow("device created successfully",
		zap.Any("identifier", identifier),
		zap.Any("user_id", userId),
	)

	return nil
}

func (l *BindDeviceLogic) rebindDeviceToNewUser(deviceInfo *user.Device, ip, userAgent string, newUserId int64) error {
	oldUserId := deviceInfo.UserId

	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		// Check if old user has other auth methods besides device
		authMethods, err := store.User().FindUserAuthMethods(l.ctx, oldUserId)
		if err != nil {
			l.Logger.Errorw("failed to query auth methods for old user",
				zap.Any("old_user_id", oldUserId),
				zap.Any("error", err.Error()),
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
			_, err = store.User().FindUserSubscribesByUserAndStatus(l.ctx, oldUserId, 0, 1)
			if err != nil {
				l.Logger.Errorw("failed to query old user subscribes",
					zap.Any("old_user_id", oldUserId),
					zap.Any("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query old user subscribes failed: %v", err)
			}
		}

		// Only disable old user if they have no other auth methods
		if nonDeviceAuthCount == 0 {
			falseVal := false
			oldUser, err := store.User().FindOne(l.ctx, oldUserId)
			if err != nil {
				l.Logger.Errorw("failed to find old user",
					zap.Any("old_user_id", oldUserId),
					zap.Any("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find old user failed: %v", err)
			}
			oldUser.Enable = &falseVal
			if err := store.User().Update(l.ctx, oldUser); err != nil {
				l.Logger.Errorw("failed to disable old user",
					zap.Any("old_user_id", oldUserId),
					zap.Any("error", err.Error()),
				)
				return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "disable old user failed: %v", err)
			}
		}

		// Update device auth method to new user
		if err := store.User().UpdateUserAuthMethodOwner(l.ctx, "device", deviceInfo.Identifier, newUserId); err != nil {
			l.Logger.Errorw("failed to update device auth method",
				zap.Any("identifier", deviceInfo.Identifier),
				zap.Any("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device auth method failed: %v", err)
		}

		// 更新设备绑定的用户id
		deviceInfo.UserId = newUserId
		deviceInfo.Ip = ip
		deviceInfo.UserAgent = userAgent
		deviceInfo.Enabled = true

		if err := store.User().UpdateDevice(l.ctx, deviceInfo); err != nil {
			l.Logger.Errorw("failed to update device",
				zap.Any("identifier", deviceInfo.Identifier),
				zap.Any("error", err.Error()),
			)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update device failed: %v", err)
		}
		return nil
	})

	if err != nil {
		l.Logger.Errorw("device rebinding failed",
			zap.Any("identifier", deviceInfo.Identifier),
			zap.Any("old_user_id", oldUserId),
			zap.Any("new_user_id", newUserId),
			zap.Any("error", err.Error()),
		)
		return err
	}

	l.Logger.Infow("device rebound successfully",
		zap.Any("identifier", deviceInfo.Identifier),
		zap.Any("old_user_id", oldUserId),
		zap.Any("new_user_id", newUserId),
	)

	return nil
}
