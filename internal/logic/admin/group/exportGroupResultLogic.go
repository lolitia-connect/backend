package group

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type ExportGroupResultLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewExportGroupResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ExportGroupResultLogic {
	return &ExportGroupResultLogic{
		Logger: zap.S(),
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
		details, err := l.svcCtx.Store.Group().QueryGroupHistoryDetails(l.ctx, *req.HistoryId)
		if err != nil {
			zap.S().Errorf("failed to get group history details: %v", err)
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
				zap.S().Errorf("failed to parse user data: %v", err)
				continue
			}

			// 查询节点组名称
			nodeGroup, _ := l.svcCtx.Store.Group().FindNodeGroup(l.ctx, detail.NodeGroupId)

			// 为每个用户生成记录
			for _, item := range users {
				nodeGroupID := detail.NodeGroupId
				nodeGroupName := ""
				if nodeGroup != nil {
					nodeGroupID = nodeGroup.Id
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
		userSubscribes, err := l.svcCtx.Store.User().FindUserSubscribesWithNodeGroup(l.ctx)
		if err != nil {
			zap.S().Errorf("failed to get users: %v", err)
			return nil, "", err
		}
		seen := make(map[string]struct{})

		// 为每个用户生成记录
		for _, us := range userSubscribes {
			if us.NodeGroupId <= 0 {
				continue
			}
			key := fmt.Sprintf("%d:%d", us.UserId, us.NodeGroupId)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			// 查询节点组信息
			nodeGroup, err := l.svcCtx.Store.Group().FindNodeGroup(l.ctx, us.NodeGroupId)
			if err != nil {
				zap.S().Errorf("failed to find node group: %v", err)
				// 跳过该用户
				continue
			}

			records = append(records, []string{
				fmt.Sprintf("%d", us.UserId),
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
		zap.S().Errorf("failed to write csv: %v", err)
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
