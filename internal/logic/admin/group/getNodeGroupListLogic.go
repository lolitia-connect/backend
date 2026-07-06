package group

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	entnode "github.com/perfect-panel/server/ent/node"
	entnodegroup "github.com/perfect-panel/server/ent/nodegroup"
	"github.com/perfect-panel/server/ent/predicate"
	"github.com/perfect-panel/server/internal/model/group"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type GetNodeGroupListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNodeGroupListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeGroupListLogic {
	return &GetNodeGroupListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodeGroupListLogic) GetNodeGroupList(req *types.GetNodeGroupListRequest) (resp *types.GetNodeGroupListResponse, err error) {
	query := l.svcCtx.Ent.NodeGroup.Query()

	// 获取总数
	total, err := query.Clone().Count(l.ctx)
	if err != nil {
		logger.Errorf("failed to count node groups: %v", err)
		return nil, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.Size
	nodeGroups, err := query.Order(ent.Asc(entnodegroup.FieldSort)).Offset(offset).Limit(req.Size).All(l.ctx)
	if err != nil {
		logger.Errorf("failed to find node groups: %v", err)
		return nil, err
	}

	// 转换为响应格式
	var list []types.NodeGroup
	for _, ng := range nodeGroups {
		// 统计该组的节点数（JSON数组查询）
		nodeCount, _ := l.svcCtx.Ent.Node.Query().Where(predicate.Node(func(s *sql.Selector) {
			s.Where(sql.P(func(b *sql.Builder) {
				b.WriteString("JSON_CONTAINS(").Ident(entnode.FieldNodeGroupIds).WriteString(", ").Arg(fmt.Sprintf("[%d]", ng.ID)).WriteByte(')')
			}))
		})).Count(l.ctx)

		// 处理指针类型的字段
		var forCalculation bool
		forCalculation = ng.ForCalculation

		isExpiredGroup := ng.IsExpiredGroup

		var minTrafficGB, maxTrafficGB, maxTrafficGBExpired int64
		if ng.MinTrafficGB != nil {
			minTrafficGB = *ng.MinTrafficGB
		}
		if ng.MaxTrafficGB != nil {
			maxTrafficGB = *ng.MaxTrafficGB
		}
		if ng.MaxTrafficGBExpired != nil {
			maxTrafficGBExpired = *ng.MaxTrafficGBExpired
		}

		list = append(list, types.NodeGroup{
			Id:                  ng.ID,
			Name:                ng.Name,
			Type:                group.MustNodeGroupType(ng.Type),
			Description:         ng.Description,
			Sort:                ng.Sort,
			ForCalculation:      forCalculation,
			IsExpiredGroup:      isExpiredGroup,
			ExpiredDaysLimit:    ng.ExpiredDaysLimit,
			MaxTrafficGBExpired: maxTrafficGBExpired,
			SpeedLimit:          ng.SpeedLimit,
			MinTrafficGB:        minTrafficGB,
			MaxTrafficGB:        maxTrafficGB,
			NodeCount:           int64(nodeCount),
			CreatedAt:           ng.CreatedAt.Unix(),
			UpdatedAt:           ng.UpdatedAt.Unix(),
		})
	}

	resp = &types.GetNodeGroupListResponse{
		Total: int64(total),
		List:  list,
	}

	return resp, nil
}
