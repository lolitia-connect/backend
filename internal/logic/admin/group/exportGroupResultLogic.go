package group

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/ent/grouphistorydetail"
	"github.com/perfect-panel/server/ent/nodegroup"
	"github.com/perfect-panel/server/ent/usersubscribe"
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
		details, err := l.svcCtx.Ent.GroupHistoryDetail.Query().Where(grouphistorydetail.HistoryID(*req.HistoryId)).All(l.ctx)
		if err != nil {
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
			if err := json.Unmarshal([]byte(detail.UserData), &users); err != nil {
				logger.Errorf("failed to parse user data: %v", err)
				continue
			}

			// 查询节点组名称
			nodeGroup, _ := l.svcCtx.Ent.NodeGroup.Query().Where(nodegroup.ID(detail.NodeGroupID)).Only(l.ctx)

			// 为每个用户生成记录
			for _, item := range users {
				nodeGroupID := detail.NodeGroupID
				nodeGroupName := ""
				if nodeGroup != nil {
					nodeGroupID = nodeGroup.ID
					nodeGroupName = nodeGroup.Name
				}
				records = append(records, []string{
					fmt.Sprintf("%d", item.Id),
					fmt.Sprintf("%d", nodeGroupID),
					nodeGroupName,
				})
			}
		}
	} else {
		// 导出当前所有用户的分组情况
		type UserNodeGroupInfo struct {
			Id          int64 `json:"id"`
			NodeGroupId int64 `json:"node_group_id"`
		}
		userSubscribes, err := l.svcCtx.Ent.UserSubscribe.Query().Where(usersubscribe.NodeGroupIDGT(0)).All(l.ctx)
		if err != nil {
			logger.Errorf("failed to get users: %v", err)
			return nil, "", err
		}
		seen := make(map[string]struct{})

		// 为每个用户生成记录
		for _, us := range userSubscribes {
			key := fmt.Sprintf("%d:%d", us.UserID, us.NodeGroupID)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			// 查询节点组信息
			nodeGroup, err := l.svcCtx.Ent.NodeGroup.Query().Where(nodegroup.ID(us.NodeGroupID)).Only(l.ctx)
			if err != nil {
				logger.Errorf("failed to find node group: %v", err)
				// 跳过该用户
				continue
			}

			records = append(records, []string{
				fmt.Sprintf("%d", us.UserID),
				fmt.Sprintf("%d", nodeGroup.ID),
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
