package group

import (
	"context"

	model "github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"go.uber.org/zap"
)

type GetGroupHistoryLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetGroupHistoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetGroupHistoryLogic {
	return &GetGroupHistoryLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetGroupHistoryLogic) GetGroupHistory(req *types.GetGroupHistoryRequest) (resp *types.GetGroupHistoryResponse, err error) {
	total, histories, err := l.svcCtx.Store.Group().QueryGroupHistory(l.ctx, &model.GroupHistoryFilter{Page: req.Page, Size: req.Size, GroupMode: req.GroupMode, TriggerType: req.TriggerType})
	if err != nil {
		zap.S().Errorf("failed to find group histories: %v", err)
		return nil, err
	}

	// 转换为响应格式
	var list []types.GroupHistory
	for _, h := range histories {
		var startTime, endTime *int64
		if h.StartTime != nil {
			t := tool.UnixPtr(h.StartTime)
			startTime = &t
		}
		if h.EndTime != nil {
			t := tool.UnixPtr(h.EndTime)
			endTime = &t
		}

		list = append(list, types.GroupHistory{
			Id:           h.Id,
			GroupMode:    h.GroupMode,
			TriggerType:  h.TriggerType,
			TotalUsers:   h.TotalUsers,
			SuccessCount: h.SuccessCount,
			FailedCount:  h.FailedCount,
			StartTime:    startTime,
			EndTime:      endTime,
			ErrorLog:     h.ErrorMessage,
			CreatedAt:    tool.Unix(h.CreatedAt),
		})
	}

	resp = &types.GetGroupHistoryResponse{
		Total: total,
		List:  list,
	}

	return resp, nil
}
