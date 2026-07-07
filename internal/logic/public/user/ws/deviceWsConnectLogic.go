package ws

import (
	"context"
	"time"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/hertzx"
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

	value := l.ctx.Value(constant.CtxKeyIdentifier)
	if value == nil || value.(string) == "" {
		value, _ = c.GetQuery("identifier")
		if value == nil || value.(string) == "" {
			l.Logger.Errorf("DeviceWsConnectLogic DeviceWsConnect identifier is empty")
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "identifier is empty")
		}
	}
	identifier := value.(string)
	_, err := l.svcCtx.Store.User().FindOneDeviceByIdentifier(l.ctx, identifier)
	if err != nil && !ent.IsNotFound(err) {
		l.Logger.Errorf("DeviceWsConnectLogic DeviceWsConnect FindOneDeviceByIdentifier err: %v", err)
		return errors.Wrap(xerr.NewErrCode(xerr.DatabaseQueryError), err.Error())
	}

	value = l.ctx.Value(constant.CtxKeyUser)
	if value == nil {
		l.Logger.Errorf("DeviceWsConnectLogic DeviceWsConnect value is nil")
		return nil
	}
	userInfo := value.(*user.User)
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
			if time.Now().Before(sub.ExpireTime) {
				deviceLimit := int(sub.Subscribe.DeviceLimit)
				if deviceLimit > maxDevice {
					maxDevice = deviceLimit
				}
			}
		}
	}
	l.svcCtx.DeviceManager.AddDevice(c.Writer, c.Request, l.ctx.Value(constant.CtxKeySessionID).(string), userInfo.Id, identifier, maxDevice)
	return nil
}
