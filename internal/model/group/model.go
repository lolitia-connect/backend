package group

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/ent/grouphistory"
	"github.com/perfect-panel/server/ent/grouphistorydetail"
	entnodegroup "github.com/perfect-panel/server/ent/nodegroup"
	"github.com/perfect-panel/server/ent/predicate"
)

type Model interface {
	CreateNodeGroup(ctx context.Context, data *NodeGroup) (*NodeGroup, error)
	FindNodeGroup(ctx context.Context, id int64) (*NodeGroup, error)
	UpdateNodeGroup(ctx context.Context, data *NodeGroup) error
	DeleteNodeGroup(ctx context.Context, id int64) error
	QueryNodeGroupList(ctx context.Context, page, size int) (int64, []*NodeGroup, error)
	QueryAllNodeGroups(ctx context.Context) ([]*NodeGroup, error)
	QueryNodeGroupsByIds(ctx context.Context, ids []int64) ([]*NodeGroup, error)
	CountExpiredNodeGroups(ctx context.Context, excludeId ...int64) (int64, error)
	FindExpiredNodeGroup(ctx context.Context) (*NodeGroup, error)
	QueryCalculationNodeGroups(ctx context.Context) ([]*NodeGroup, error)
	QueryTrafficCalculationNodeGroups(ctx context.Context) ([]*NodeGroup, error)
	QueryPositiveTrafficCalculationNodeGroups(ctx context.Context) ([]*NodeGroup, error)
	QueryTrafficRangeNodeGroups(ctx context.Context, excludeId int64) ([]*NodeGroup, error)
	CountNodesByNodeGroupId(ctx context.Context, nodeGroupId int64) (int64, error)
	ClearAllNodeGroups(ctx context.Context) error
	QueryGroupHistory(ctx context.Context, params *GroupHistoryFilter) (int64, []*GroupHistory, error)
	FindGroupHistory(ctx context.Context, id int64) (*GroupHistory, error)
	FindLatestGroupHistory(ctx context.Context) (*GroupHistory, error)
	CreateGroupHistory(ctx context.Context, data *GroupHistory) (*GroupHistory, error)
	UpdateGroupHistoryRunning(ctx context.Context, id int64) error
	CompleteGroupHistory(ctx context.Context, id int64, totalUsers, successCount, failedCount int, endTime time.Time) error
	FailGroupHistory(ctx context.Context, id int64, errorMessage string, endTime time.Time) error
	ClearGroupHistories(ctx context.Context) error
	QueryGroupHistoryDetails(ctx context.Context, historyId int64) ([]*GroupHistoryDetail, error)
	CreateGroupHistoryDetail(ctx context.Context, data *GroupHistoryDetail) error
	ClearGroupHistoryDetails(ctx context.Context) error
}

type GroupHistoryFilter struct {
	Page        int
	Size        int
	GroupMode   string
	TriggerType string
}

type customGroupModel struct{ db *ent.Client }

func NewModel(conn *ent.Client) Model { return &customGroupModel{db: conn} }

func (m *customGroupModel) CreateNodeGroup(ctx context.Context, data *NodeGroup) (*NodeGroup, error) {
	created, err := m.db.NodeGroup.Create().
		SetName(data.Name).
		SetType(MustNodeGroupType(data.Type)).
		SetDescription(data.Description).
		SetSort(data.Sort).
		SetNillableForCalculation(data.ForCalculation).
		SetNillableIsExpiredGroup(data.IsExpiredGroup).
		SetExpiredDaysLimit(data.ExpiredDaysLimit).
		SetNillableMaxTrafficGBExpired(data.MaxTrafficGBExpired).
		SetSpeedLimit(data.SpeedLimit).
		SetNillableMinTrafficGB(data.MinTrafficGB).
		SetNillableMaxTrafficGB(data.MaxTrafficGB).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return nodeGroupFromEnt(created), nil
}

func (m *customGroupModel) FindNodeGroup(ctx context.Context, id int64) (*NodeGroup, error) {
	data, err := m.db.NodeGroup.Query().Where(entnodegroup.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return nodeGroupFromEnt(data), nil
}

func (m *customGroupModel) UpdateNodeGroup(ctx context.Context, data *NodeGroup) error {
	update := m.db.NodeGroup.UpdateOneID(data.Id).
		SetName(data.Name).
		SetType(MustNodeGroupType(data.Type)).
		SetDescription(data.Description).
		SetSort(data.Sort).
		SetNillableForCalculation(data.ForCalculation).
		SetNillableIsExpiredGroup(data.IsExpiredGroup).
		SetExpiredDaysLimit(data.ExpiredDaysLimit).
		SetNillableMaxTrafficGBExpired(data.MaxTrafficGBExpired).
		SetSpeedLimit(data.SpeedLimit).
		SetNillableMinTrafficGB(data.MinTrafficGB).
		SetNillableMaxTrafficGB(data.MaxTrafficGB).
		SetUpdatedAt(time.Now())
	return update.Exec(ctx)
}

func (m *customGroupModel) DeleteNodeGroup(ctx context.Context, id int64) error {
	_, err := m.db.NodeGroup.Delete().Where(entnodegroup.ID(id)).Exec(ctx)
	return err
}

func (m *customGroupModel) QueryNodeGroupList(ctx context.Context, page, size int) (int64, []*NodeGroup, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	query := m.db.NodeGroup.Query()
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Order(entnodegroup.BySort()).Offset((page - 1) * size).Limit(size).All(ctx)
	return int64(total), nodeGroupsFromEnt(items), err
}

func (m *customGroupModel) QueryAllNodeGroups(ctx context.Context) ([]*NodeGroup, error) {
	items, err := m.db.NodeGroup.Query().All(ctx)
	return nodeGroupsFromEnt(items), err
}

func (m *customGroupModel) QueryNodeGroupsByIds(ctx context.Context, ids []int64) ([]*NodeGroup, error) {
	items, err := m.db.NodeGroup.Query().Where(entnodegroup.IDIn(ids...)).All(ctx)
	return nodeGroupsFromEnt(items), err
}

func (m *customGroupModel) CountExpiredNodeGroups(ctx context.Context, excludeId ...int64) (int64, error) {
	query := m.db.NodeGroup.Query().Where(entnodegroup.IsExpiredGroup(true))
	if len(excludeId) > 0 {
		query = query.Where(entnodegroup.IDNEQ(excludeId[0]))
	}
	count, err := query.Count(ctx)
	return int64(count), err
}

func (m *customGroupModel) FindExpiredNodeGroup(ctx context.Context) (*NodeGroup, error) {
	data, err := m.db.NodeGroup.Query().Where(entnodegroup.IsExpiredGroup(true)).First(ctx)
	if err != nil {
		return nil, err
	}
	return nodeGroupFromEnt(data), nil
}

func (m *customGroupModel) QueryCalculationNodeGroups(ctx context.Context) ([]*NodeGroup, error) {
	items, err := m.db.NodeGroup.Query().Where(entnodegroup.ForCalculation(true)).All(ctx)
	return nodeGroupsFromEnt(items), err
}

func (m *customGroupModel) QueryTrafficCalculationNodeGroups(ctx context.Context) ([]*NodeGroup, error) {
	items, err := m.db.NodeGroup.Query().Where(entnodegroup.ForCalculation(true), entnodegroup.Or(entnodegroup.MinTrafficGBNotNil(), entnodegroup.MaxTrafficGBNotNil())).All(ctx)
	return nodeGroupsFromEnt(items), err
}

func (m *customGroupModel) QueryPositiveTrafficCalculationNodeGroups(ctx context.Context) ([]*NodeGroup, error) {
	items, err := m.db.NodeGroup.Query().Where(entnodegroup.ForCalculation(true), entnodegroup.Or(entnodegroup.MinTrafficGBGT(0), entnodegroup.MaxTrafficGBGT(0))).All(ctx)
	return nodeGroupsFromEnt(items), err
}

func (m *customGroupModel) QueryTrafficRangeNodeGroups(ctx context.Context, excludeId int64) ([]*NodeGroup, error) {
	query := m.db.NodeGroup.Query().Where(entnodegroup.Or(entnodegroup.MinTrafficGBNotNil(), entnodegroup.MaxTrafficGBNotNil()))
	if excludeId > 0 {
		query = query.Where(entnodegroup.IDNEQ(excludeId))
	}
	items, err := query.All(ctx)
	return nodeGroupsFromEnt(items), err
}

func (m *customGroupModel) CountNodesByNodeGroupId(ctx context.Context, nodeGroupId int64) (int64, error) {
	count, err := m.db.Node.Query().Where(nodeGroupContains(nodeGroupId)).Count(ctx)
	return int64(count), err
}

func (m *customGroupModel) ClearAllNodeGroups(ctx context.Context) error {
	_, err := m.db.NodeGroup.Delete().Exec(ctx)
	return err
}

func (m *customGroupModel) QueryGroupHistory(ctx context.Context, params *GroupHistoryFilter) (int64, []*GroupHistory, error) {
	if params == nil {
		params = &GroupHistoryFilter{Page: 1, Size: 10}
	}
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Size < 1 {
		params.Size = 10
	}
	query := m.db.GroupHistory.Query()
	if params.GroupMode != "" {
		query = query.Where(grouphistory.GroupMode(params.GroupMode))
	}
	if params.TriggerType != "" {
		query = query.Where(grouphistory.TriggerType(params.TriggerType))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return 0, nil, err
	}
	items, err := query.Order(ent.Desc(grouphistory.FieldID)).Offset((params.Page - 1) * params.Size).Limit(params.Size).All(ctx)
	return int64(total), groupHistoriesFromEnt(items), err
}

func (m *customGroupModel) FindGroupHistory(ctx context.Context, id int64) (*GroupHistory, error) {
	data, err := m.db.GroupHistory.Query().Where(grouphistory.ID(id)).Only(ctx)
	if err != nil {
		return nil, err
	}
	return groupHistoryFromEnt(data), nil
}

func (m *customGroupModel) FindLatestGroupHistory(ctx context.Context) (*GroupHistory, error) {
	data, err := m.db.GroupHistory.Query().Order(ent.Desc(grouphistory.FieldID)).First(ctx)
	if err != nil {
		return nil, err
	}
	return groupHistoryFromEnt(data), nil
}

func (m *customGroupModel) CreateGroupHistory(ctx context.Context, data *GroupHistory) (*GroupHistory, error) {
	created, err := m.db.GroupHistory.Create().SetGroupMode(data.GroupMode).SetTriggerType(data.TriggerType).SetState(data.State).SetTotalUsers(data.TotalUsers).SetSuccessCount(data.SuccessCount).SetFailedCount(data.FailedCount).SetNillableStartTime(data.StartTime).SetNillableEndTime(data.EndTime).SetOperator(data.Operator).SetErrorMessage(data.ErrorMessage).Save(ctx)
	if err != nil {
		return nil, err
	}
	return groupHistoryFromEnt(created), nil
}

func (m *customGroupModel) UpdateGroupHistoryRunning(ctx context.Context, id int64) error {
	return m.db.GroupHistory.UpdateOneID(id).SetState("running").Exec(ctx)
}

func (m *customGroupModel) CompleteGroupHistory(ctx context.Context, id int64, totalUsers, successCount, failedCount int, endTime time.Time) error {
	return m.db.GroupHistory.UpdateOneID(id).SetState("completed").SetTotalUsers(totalUsers).SetSuccessCount(successCount).SetFailedCount(failedCount).SetEndTime(endTime).Exec(ctx)
}

func (m *customGroupModel) FailGroupHistory(ctx context.Context, id int64, errorMessage string, endTime time.Time) error {
	return m.db.GroupHistory.UpdateOneID(id).SetState("failed").SetErrorMessage(errorMessage).SetEndTime(endTime).Exec(ctx)
}

func (m *customGroupModel) ClearGroupHistories(ctx context.Context) error {
	_, err := m.db.GroupHistory.Delete().Exec(ctx)
	return err
}

func (m *customGroupModel) QueryGroupHistoryDetails(ctx context.Context, historyId int64) ([]*GroupHistoryDetail, error) {
	items, err := m.db.GroupHistoryDetail.Query().Where(grouphistorydetail.HistoryID(historyId)).All(ctx)
	return groupHistoryDetailsFromEnt(items), err
}

func (m *customGroupModel) CreateGroupHistoryDetail(ctx context.Context, data *GroupHistoryDetail) error {
	return m.db.GroupHistoryDetail.Create().SetHistoryID(data.HistoryId).SetNodeGroupID(data.NodeGroupId).SetUserCount(data.UserCount).SetNodeCount(data.NodeCount).SetUserData(data.UserData).Exec(ctx)
}

func (m *customGroupModel) ClearGroupHistoryDetails(ctx context.Context) error {
	_, err := m.db.GroupHistoryDetail.Delete().Exec(ctx)
	return err
}

func nodeGroupContains(id int64) predicate.Node {
	return predicate.Node(func(s *sql.Selector) {
		s.Where(sql.ExprP("JSON_CONTAINS(node_group_ids, ?)", fmt.Sprintf("[%d]", id)))
	})
}

func nodeGroupFromEnt(data *ent.NodeGroup) *NodeGroup {
	if data == nil {
		return nil
	}
	forCalculation := data.ForCalculation
	isExpiredGroup := data.IsExpiredGroup
	return &NodeGroup{Id: data.ID, Name: data.Name, Type: data.Type, Description: data.Description, Sort: data.Sort, ForCalculation: &forCalculation, IsExpiredGroup: &isExpiredGroup, ExpiredDaysLimit: data.ExpiredDaysLimit, MaxTrafficGBExpired: data.MaxTrafficGBExpired, SpeedLimit: data.SpeedLimit, MinTrafficGB: data.MinTrafficGB, MaxTrafficGB: data.MaxTrafficGB, CreatedAt: data.CreatedAt, UpdatedAt: data.UpdatedAt}
}

func nodeGroupsFromEnt(items []*ent.NodeGroup) []*NodeGroup {
	list := make([]*NodeGroup, 0, len(items))
	for _, item := range items {
		list = append(list, nodeGroupFromEnt(item))
	}
	return list
}

func groupHistoryFromEnt(data *ent.GroupHistory) *GroupHistory {
	if data == nil {
		return nil
	}
	return &GroupHistory{Id: data.ID, GroupMode: data.GroupMode, TriggerType: data.TriggerType, State: data.State, TotalUsers: data.TotalUsers, SuccessCount: data.SuccessCount, FailedCount: data.FailedCount, StartTime: data.StartTime, EndTime: data.EndTime, Operator: data.Operator, ErrorMessage: data.ErrorMessage, CreatedAt: data.CreatedAt}
}

func groupHistoriesFromEnt(items []*ent.GroupHistory) []*GroupHistory {
	list := make([]*GroupHistory, 0, len(items))
	for _, item := range items {
		list = append(list, groupHistoryFromEnt(item))
	}
	return list
}

func groupHistoryDetailFromEnt(data *ent.GroupHistoryDetail) *GroupHistoryDetail {
	if data == nil {
		return nil
	}
	return &GroupHistoryDetail{Id: data.ID, HistoryId: data.HistoryID, NodeGroupId: data.NodeGroupID, UserCount: data.UserCount, NodeCount: data.NodeCount, UserData: data.UserData, CreatedAt: data.CreatedAt}
}

func groupHistoryDetailsFromEnt(items []*ent.GroupHistoryDetail) []*GroupHistoryDetail {
	list := make([]*GroupHistoryDetail, 0, len(items))
	for _, item := range items {
		list = append(list, groupHistoryDetailFromEnt(item))
	}
	return list
}
