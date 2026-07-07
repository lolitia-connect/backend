package group

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type GetRecalculationStatusLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get recalculation status
func NewGetRecalculationStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRecalculationStatusLogic {
	return &GetRecalculationStatusLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRecalculationStatusLogic) GetRecalculationStatus() (resp *types.RecalculationState, err error) {
	// 返回最近的一条 GroupHistory 记录
	history, err := l.svcCtx.Store.Group().FindLatestGroupHistory(l.ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// 如果没有历史记录，返回空闲状态
			resp = &types.RecalculationState{
				State:    "idle",
				Progress: 0,
				Total:    0,
			}
			return resp, nil
		}
		l.Logger.Errorw("failed to get group history", zap.Any("error", err.Error()))
		return nil, err
	}

	// 转换为 RecalculationState 格式
	// Progress = 已处理的用户数（成功+失败），Total = 总用户数
	processedUsers := history.SuccessCount + history.FailedCount
	resp = &types.RecalculationState{
		State:    history.State,
		Progress: processedUsers,
		Total:    history.TotalUsers,
	}

	return resp, nil
}
