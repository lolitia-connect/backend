package group

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"

	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type ExportGroupResultLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewExportGroupResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ExportGroupResultLogic {
	return &ExportGroupResultLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ExportGroupResult 导出分组结果为 CSV
// 返回：CSV 数据（字节切片）、文件名、错误
func (l *ExportGroupResultLogic) ExportGroupResult(req *types.ExportGroupResultRequest) ([]byte, string, error) {
	var records [][]string

	// CSV 表头
	records = append(records, []string{"用户ID", "节点组ID", "节点组名称"})

	if req.HistoryId != nil {
		// 导出指定历史的详细结果
		// 1. 查询分组历史详情
		var details []group.GroupHistoryDetail
		if err := l.svcCtx.DB.Where("history_id = ?", *req.HistoryId).Find(&details).Error; err != nil {
			logger.Errorf("failed to get group history details: %v", err)
			return nil, "", err
		}

		// 2. 为每个组生成记录
		for _, detail := range details {
			// 从 UserData JSON 解析用户信息
			type UserInfo struct {
				Id    int64  `json:"id"`
				Email string `json:"email"`
			}
			var users []UserInfo
			if err := l.svcCtx.DB.Raw("SELECT * FROM JSON_ARRAY(?)", detail.UserData).Scan(&users).Error; err != nil {
				// 如果解析失败，尝试用标准 JSON 解析
				logger.Errorf("failed to parse user data: %v", err)
				continue
			}

			// 查询节点组名称
			var nodeGroup group.NodeGroup
			l.svcCtx.DB.Where("id = ?", detail.NodeGroupId).First(&nodeGroup)

			// 为每个用户生成记录
			for _, user := range users {
				records = append(records, []string{
					fmt.Sprintf("%d", user.Id),
					fmt.Sprintf("%d", nodeGroup.Id),
					nodeGroup.Name,
				})
			}
		}
	} else {
		// 导出当前所有用户的分组情况
		type UserNodeGroupInfo struct {
			Id          int64 `json:"id"`
			NodeGroupId int64 `json:"node_group_id"`
		}
		var userSubscribes []UserNodeGroupInfo
		if err := l.svcCtx.DB.Table("user_subscribe").
			Select("DISTINCT user_id as id, node_group_id").
			Where("node_group_id > ?", 0).
			Find(&userSubscribes).Error; err != nil {
			logger.Errorf("failed to get users: %v", err)
			return nil, "", err
		}

		// 为每个用户生成记录
		for _, us := range userSubscribes {
			// 查询节点组信息
			var nodeGroup group.NodeGroup
			if err := l.svcCtx.DB.Where("id = ?", us.NodeGroupId).First(&nodeGroup).Error; err != nil {
				logger.Errorf("failed to find node group: %v", err)
				// 跳过该用户
				continue
			}

			records = append(records, []string{
				fmt.Sprintf("%d", us.Id),
				fmt.Sprintf("%d", nodeGroup.Id),
				nodeGroup.Name,
			})
		}
	}

	// 生成 CSV 数据
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	writer.WriteAll(records)
	writer.Flush()

	if err := writer.Error(); err != nil {
		logger.Errorf("failed to write csv: %v", err)
		return nil, "", err
	}

	// 添加 UTF-8 BOM
	bom := []byte{0xEF, 0xBB, 0xBF}
	csvData := buf.Bytes()
	result := make([]byte, 0, len(bom)+len(csvData))
	result = append(result, bom...)
	result = append(result, csvData...)

	// 生成文件名
	filename := fmt.Sprintf("group_result_%d.csv", req.HistoryId)

	return result, filename, nil
}
