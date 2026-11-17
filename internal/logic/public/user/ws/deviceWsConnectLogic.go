package ws

import (
	"context"
	"strconv"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

type DeviceWsConnectLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Webosocket Device Connect
func NewDeviceWsConnectLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeviceWsConnectLogic {
	return &DeviceWsConnectLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeviceWsConnectLogic) DeviceWsConnect(c *hertzx.Context) error {
	identifier, _ := l.ctx.Value(constant.CtxKeyIdentifier).(string)
	if identifier == "" {
		identifier = c.Param("identifier")
	}
	if identifier == "" {
		identifier = c.Query("identifier")
	}
	if identifier == "" || len(identifier) > 255 {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid device identifier")
	}
	userInfo, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	session, sessionOK := l.ctx.Value(constant.CtxKeySessionID).(string)
	if !ok || userInfo == nil || !sessionOK || session == "" {
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	if pathID := c.Param("userid"); pathID != "" && pathID != strconv.FormatInt(userInfo.Id, 10) {
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	oneDevice, err := l.svcCtx.Store.User().FindOneDeviceByIdentifier(l.ctx, identifier)
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorf("DeviceWsConnectLogic DeviceWsConnect FindOneDeviceByIdentifier err: %v", err)
		return errors.Wrap(xerr.NewErrCode(xerr.DatabaseQueryError), err.Error())
	}

	if oneDevice != nil && (oneDevice.UserId != userInfo.Id || !oneDevice.Enabled) {
		return xerr.NewErrCode(xerr.InvalidAccess)
	}
	if ent.IsNotFound(err) {
		device := user.Device{
			Identifier: identifier,
			UserId:     userInfo.Id,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Online:     true,
			Enabled:    true,
		}
		err := l.svcCtx.Store.User().InsertDevice(l.ctx, &device)
		if err != nil {
			l.Logger.Errorf("DeviceWsConnectLogic DeviceWsConnect InsertDevice err: %v", err)
			return errors.Wrap(xerr.NewErrCode(xerr.DatabaseInsertError), err.Error())
		}
	}
	//默认在线设备1
	maxDevice := 3
	subscribe, err := l.svcCtx.Store.User().QueryUserSubscribe(l.ctx, userInfo.Id, 1, 2)
	if err == nil {
		for _, sub := range subscribe {
			if sub.Subscribe != nil && (tool.Unix(sub.ExpireTime) == 0 || time.Now().Before(sub.ExpireTime)) {
				deviceLimit := int(sub.Subscribe.DeviceLimit)
				if deviceLimit > maxDevice {
					maxDevice = deviceLimit
				}
			}
		}
	}
	if l.svcCtx.DeviceManager == nil {
		return xerr.NewErrCode(xerr.ERROR)
	}
	return l.svcCtx.DeviceManager.UpgradeHertz(c.Raw(), session, userInfo.Id, identifier, maxDevice)
}
