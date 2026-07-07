package authMethod

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/sms"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type TestSmsSendLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Test sms send
func NewTestSmsSendLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TestSmsSendLogic {
	return &TestSmsSendLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TestSmsSendLogic) TestSmsSend(req *types.TestSmsSendRequest) error {
	client, err := sms.NewSender(l.svcCtx.Config.Mobile.Platform, l.svcCtx.Config.Mobile.PlatformConfig)
	if err != nil {
		l.Logger.Errorw("new sms sender err", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "new sms sender err: %v", err.Error())
	}
	err = client.SendCode(req.AreaCode, req.Telephone, "123456")
	if err != nil {
		l.Logger.Errorw("send sms err", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCodeMsg(500, fmt.Sprintf("send sms err: %v", err.Error())), "send sms err: %v", err.Error())
	}
	return nil
}
