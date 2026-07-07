package marketing

import (
	"context"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

type QueryQuotaTaskPreCountLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryQuotaTaskPreCountLogic Query quota task pre-count
func NewQueryQuotaTaskPreCountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryQuotaTaskPreCountLogic {
	return &QueryQuotaTaskPreCountLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryQuotaTaskPreCountLogic) QueryQuotaTaskPreCount(req *types.QueryQuotaTaskPreCountRequest) (resp *types.QueryQuotaTaskPreCountResponse, err error) {
	count, err := l.svcCtx.Store.User().CountSubscribesByFilter(l.ctx, &user.SubscribeFilter{
		Subscribers: tool.StringSliceToInt64Slice(req.Subscribers),
		IsActive:    req.IsActive,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	})
	if err != nil {
		l.Logger.Errorf("[QueryQuotaTaskPreCount] count error: %v", err.Error())
		return nil, err
	}

	return &types.QueryQuotaTaskPreCountResponse{
		Count: count,
	}, nil
}
