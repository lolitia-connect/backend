package subscribe

import (
	"context"

	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateSubscribeGroupLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create subscribe group
func NewCreateSubscribeGroupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSubscribeGroupLogic {
	return &CreateSubscribeGroupLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateSubscribeGroupLogic) CreateSubscribeGroup(req *types.CreateSubscribeGroupRequest) error {
	err := l.svcCtx.Store.Subscribe().CreateGroup(l.ctx, &subscribe.Group{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		l.Logger.Error("[CreateSubscribeGroupLogic] create subscribe group failed: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create subscribe group failed: %v", err.Error())
	}
	return nil
}
