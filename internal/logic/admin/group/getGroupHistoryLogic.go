package group

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/grouphistory"
	"github.com/perfect-panel/server/ent/predicate"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

type GetGroupHistoryLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetGroupHistoryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetGroupHistoryLogic {
	return &GetGroupHistoryLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetGroupHistoryLogic) GetGroupHistory(req *types.GetGroupHistoryRequest) (resp *types.GetGroupHistoryResponse, err error) {
	query := l.svcCtx.Ent.GroupHistory.Query()
	predicates := make([]predicate.GroupHistory, 0, 2)

	// 添加过滤条件
	if req.GroupMode != "" {
		predicates = append(predicates, grouphistory.GroupMode(req.GroupMode))
	}
	if req.TriggerType != "" {
		predicates = append(predicates, grouphistory.TriggerType(req.TriggerType))
	}
	if len(predicates) > 0 {
		query = query.Where(predicates...)
	}

	// 获取总数
	total, err := query.Clone().Count(l.ctx)
	if err != nil {
		logger.Errorf("failed to count group histories: %v", err)
		return nil, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.Size
	histories, err := query.Order(ent.Desc(grouphistory.FieldID)).Offset(offset).Limit(req.Size).All(l.ctx)
	if err != nil {
		logger.Errorf("failed to find group histories: %v", err)
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
			Id:           h.ID,
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
		Total: int64(total),
		List:  list,
	}

	return resp, nil
}
