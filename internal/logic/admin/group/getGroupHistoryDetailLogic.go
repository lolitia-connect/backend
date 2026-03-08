package group

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"gorm.io/gorm"
)

type GetGroupHistoryDetailLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetGroupHistoryDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetGroupHistoryDetailLogic {
	return &GetGroupHistoryDetailLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetGroupHistoryDetailLogic) GetGroupHistoryDetail(req *types.GetGroupHistoryDetailRequest) (resp *types.GetGroupHistoryDetailResponse, err error) {
	// 查询分组历史记录
	var history group.GroupHistory
	if err := l.svcCtx.DB.Where("id = ?", req.Id).First(&history).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("group history not found")
		}
		logger.Errorf("failed to find group history: %v", err)
		return nil, err
	}

	// 查询分组历史详情
	var details []group.GroupHistoryDetail
	if err := l.svcCtx.DB.Where("history_id = ?", req.Id).Find(&details).Error; err != nil {
		logger.Errorf("failed to find group history details: %v", err)
		return nil, err
	}

	// 转换时间格式
	var startTime, endTime *int64
	if history.StartTime != nil {
		t := history.StartTime.Unix()
		startTime = &t
	}
	if history.EndTime != nil {
		t := history.EndTime.Unix()
		endTime = &t
	}

	// 构建 GroupHistoryDetail
	historyDetail := types.GroupHistoryDetail{
		GroupHistory: types.GroupHistory{
			Id:           history.Id,
			GroupMode:    history.GroupMode,
			TriggerType:  history.TriggerType,
			TotalUsers:   history.TotalUsers,
			SuccessCount: history.SuccessCount,
			FailedCount:  history.FailedCount,
			StartTime:    startTime,
			EndTime:      endTime,
			ErrorLog:     history.ErrorMessage,
			CreatedAt:    history.CreatedAt.Unix(),
		},
	}

	// 如果有详情记录，构建 ConfigSnapshot
	if len(details) > 0 {
		configSnapshot := make(map[string]interface{})
		configSnapshot["group_details"] = details

		// 获取配置快照（从 system_config 读取）
		var configValue string
		if history.GroupMode == "average" {
			l.svcCtx.DB.Table("system_config").
				Where("`key` = ?", "group.average_config").
				Select("value").
				Scan(&configValue)
		} else if history.GroupMode == "traffic" {
			l.svcCtx.DB.Table("system_config").
				Where("`key` = ?", "group.traffic_config").
				Select("value").
				Scan(&configValue)
		}

		// 解析 JSON 配置
		if configValue != "" {
			var config map[string]interface{}
			if err := json.Unmarshal([]byte(configValue), &config); err == nil {
				configSnapshot["config"] = config
			}
		}

		historyDetail.ConfigSnapshot = configSnapshot
	}

	resp = &types.GetGroupHistoryDetailResponse{
		GroupHistoryDetail: historyDetail,
	}

	return resp, nil
}
