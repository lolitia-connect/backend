package group

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/grouphistory"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type GetRecalculationStatusLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get recalculation status
func NewGetRecalculationStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetRecalculationStatusLogic {
	return &GetRecalculationStatusLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetRecalculationStatusLogic) GetRecalculationStatus() (resp *types.RecalculationState, err error) {
	// 返回最近的一条 GroupHistory 记录
	history, err := l.svcCtx.Ent.GroupHistory.Query().Order(ent.Desc(grouphistory.FieldID)).First(l.ctx)
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
		l.Errorw("failed to get group history", logger.Field("error", err.Error()))
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
