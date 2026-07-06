package group

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/grouphistory"
	"github.com/perfect-panel/server/ent/grouphistorydetail"
	entsystem "github.com/perfect-panel/server/ent/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
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
	history, err := l.svcCtx.Ent.GroupHistory.Query().Where(grouphistory.ID(req.Id)).Only(l.ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("group history not found")
		}
		logger.Errorf("failed to find group history: %v", err)
		return nil, err
	}

	// 查询分组历史详情
	details, err := l.svcCtx.Ent.GroupHistoryDetail.Query().Where(grouphistorydetail.HistoryID(req.Id)).All(l.ctx)
	if err != nil {
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
			Id:           history.ID,
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
		var configKey string
		if history.GroupMode == "average" {
			configKey = "average_config"
		} else if history.GroupMode == "traffic" {
			configKey = "traffic_config"
		}
		if configKey != "" {
			config, err := l.svcCtx.Ent.System.Query().Where(
				entsystem.Or(
					entsystem.Key("group."+configKey),
					entsystem.And(entsystem.Category("group"), entsystem.Key(configKey)),
				),
			).First(l.ctx)
			if err == nil {
				configValue = config.Value
			}
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
