package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"go.uber.org/zap"
)

type GetPreSendEmailCountLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetPreSendEmailCountLogic Get pre-send email count
func NewGetPreSendEmailCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPreSendEmailCountLogic {
	return &GetPreSendEmailCountLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPreSendEmailCountLogic) GetPreSendEmailCount(req *types.GetPreSendEmailCountRequest) (resp *types.GetPreSendEmailCountResponse, err error) {
	scope := task.ParseScopeType(req.Scope)
	count, err := l.svcCtx.Store.User().CountEmailRecipients(l.ctx, &user.EmailRecipientFilter{
		Scope:             scope.Int8(),
		RegisterStartTime: req.RegisterStartTime,
		RegisterEndTime:   req.RegisterEndTime,
	})
	if err != nil {
		l.Logger.Errorf("[GetPreSendEmailCount] Count error: %v", err)
		return nil, xerr.NewErrMsg("Failed to count emails")
	}

	return &types.GetPreSendEmailCountResponse{
		Count: count,
	}, nil
}
