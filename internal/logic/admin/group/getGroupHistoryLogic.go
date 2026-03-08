package group

import (
	"context"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
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
	var histories []group.GroupHistory
	var total int64

	// 构建查询
	query := l.svcCtx.DB.Model(&group.GroupHistory{})

	// 添加过滤条件
	if req.GroupMode != "" {
		query = query.Where("group_mode = ?", req.GroupMode)
	}
	if req.TriggerType != "" {
		query = query.Where("trigger_type = ?", req.TriggerType)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		logger.Errorf("failed to count group histories: %v", err)
		return nil, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.Size
	if err := query.Order("id DESC").Offset(offset).Limit(req.Size).Find(&histories).Error; err != nil {
		logger.Errorf("failed to find group histories: %v", err)
		return nil, err
	}

	// 转换为响应格式
	var list []types.GroupHistory
	for _, h := range histories {
		var startTime, endTime *int64
		if h.StartTime != nil {
			t := h.StartTime.Unix()
			startTime = &t
		}
		if h.EndTime != nil {
			t := h.EndTime.Unix()
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
			CreatedAt:    h.CreatedAt.Unix(),
		})
	}

	resp = &types.GetGroupHistoryResponse{
		Total: total,
		List:  list,
	}

	return resp, nil
}
