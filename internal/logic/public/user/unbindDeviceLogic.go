package user

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UnbindDeviceLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Unbind Device
func NewUnbindDeviceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnbindDeviceLogic {
	return &UnbindDeviceLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnbindDeviceLogic) UnbindDevice(req *types.UnbindDeviceRequest) error {
	userInfo := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	device, err := l.svcCtx.Store.User().FindOneDevice(l.ctx, req.Id)
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.DeviceNotExist), "find device")
	}

	if device.UserId != userInfo.Id {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "device not belong to user")
	}

	return l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		if err = store.User().DeleteDevice(l.ctx, req.Id); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete device err: %v", err)
		}

		if err = store.User().DeleteUserAuthMethodByIdentifier(l.ctx, "device", device.Identifier); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find device online record err: %v", err)
		}
		count, err := store.User().CountUserAuthMethods(l.ctx, device.UserId)
		if err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "count user auth methods err: %v", err)
		}

		if count < 1 {
			_ = store.User().Delete(l.ctx, device.UserId)
		}

		//remove device cache
		deviceCacheKey := fmt.Sprintf("%v:%v", config.DeviceCacheKeyKey, device.Identifier)
		if sessionId, err := l.svcCtx.Redis.Get(l.ctx, deviceCacheKey).Result(); err == nil && sessionId != "" {
			_ = l.svcCtx.Redis.Del(l.ctx, deviceCacheKey).Err()
			sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
			_ = l.svcCtx.Redis.Del(l.ctx, sessionIdCacheKey).Err()
		}
		return nil
	})
}
