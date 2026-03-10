package subscribe

import (
	"context"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryUserSubscribeNodeListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get user subscribe node info
func NewQueryUserSubscribeNodeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryUserSubscribeNodeListLogic {
	return &QueryUserSubscribeNodeListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryUserSubscribeNodeListLogic) QueryUserSubscribeNodeList() (resp *types.QueryUserSubscribeNodeListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}

	userSubscribes, err := l.svcCtx.UserModel.QueryUserSubscribe(l.ctx, u.Id, 0, 1, 2, 3)
	if err != nil {
		logger.Errorw("failed to query user subscribe", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "DB_ERROR")
	}

	resp = &types.QueryUserSubscribeNodeListResponse{}
	for _, us := range userSubscribes {
		userSubscribe, err := l.getUserSubscribe(us.Token)
		if err != nil {
			l.Errorw("[SubscribeLogic] Get user subscribe failed", logger.Field("error", err.Error()), logger.Field("token", userSubscribe.Token))
			return nil, err
		}
		nodes, err := l.getServers(userSubscribe)
		if err != nil {
			return nil, err
		}
		userSubscribeInfo := types.UserSubscribeInfo{
			Id:          userSubscribe.Id,
			Nodes:       nodes,
			Traffic:     userSubscribe.Traffic,
			Upload:      userSubscribe.Upload,
			Download:    userSubscribe.Download,
			Token:       userSubscribe.Token,
			UserId:      userSubscribe.UserId,
			OrderId:     userSubscribe.OrderId,
			SubscribeId: userSubscribe.SubscribeId,
			StartTime:   userSubscribe.StartTime.Unix(),
			ExpireTime:  userSubscribe.ExpireTime.Unix(),
			Status:      userSubscribe.Status,
			CreatedAt:   userSubscribe.CreatedAt.Unix(),
			UpdatedAt:   userSubscribe.UpdatedAt.Unix(),
		}

		if userSubscribe.FinishedAt != nil {
			userSubscribeInfo.FinishedAt = userSubscribe.FinishedAt.Unix()
		}

		if l.svcCtx.Config.Register.EnableTrial && l.svcCtx.Config.Register.TrialSubscribe == userSubscribe.SubscribeId {
			userSubscribeInfo.IsTryOut = true
		}
		resp.List = append(resp.List, userSubscribeInfo)
	}

	return
}

func (l *QueryUserSubscribeNodeListLogic) getServers(userSub *user.Subscribe) (userSubscribeNodes []*types.UserSubscribeNodeInfo, err error) {
	userSubscribeNodes = make([]*types.UserSubscribeNodeInfo, 0)
	if l.isSubscriptionExpired(userSub) {
		return l.createExpiredServers(), nil
	}

	// Check if group management is enabled
	var groupEnabled string
	err = l.svcCtx.DB.Table("system").
		Where("`category` = ? AND `key` = ?", "group", "enabled").
		Select("value").Scan(&groupEnabled).Error

	if err != nil {
		l.Debugw("[GetServers] Failed to check group enabled", logger.Field("error", err.Error()))
		// Continue with tag-based filtering
	}

	isGroupEnabled := (groupEnabled == "true" || groupEnabled == "1")

	var nodes []*node.Node
	if isGroupEnabled {
		// Group mode: use group_ids to filter nodes
		nodes, err = l.getNodesByGroup(userSub)
		if err != nil {
			l.Errorw("[GetServers] Failed to get nodes by group", logger.Field("error", err.Error()))
			return nil, err
		}
	} else {
		// Tag mode: use node_ids and tags to filter nodes
		nodes, err = l.getNodesByTag(userSub)
		if err != nil {
			l.Errorw("[GetServers] Failed to get nodes by tag", logger.Field("error", err.Error()))
			return nil, err
		}
	}

	// Process nodes and create response
	if len(nodes) > 0 {
		var serverMapIds = make(map[int64]*node.Server)
		for _, n := range nodes {
			serverMapIds[n.ServerId] = nil
		}
		var serverIds []int64
		for k := range serverMapIds {
			serverIds = append(serverIds, k)
		}

		servers, err := l.svcCtx.NodeModel.QueryServerList(l.ctx, serverIds)
		if err != nil {
			l.Errorw("[Generate Subscribe]find server details error: %v", logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server details error: %v", err.Error())
		}

		for _, s := range servers {
			serverMapIds[s.Id] = s
		}

		for _, n := range nodes {
			server := serverMapIds[n.ServerId]
			if server == nil {
				continue
			}
			userSubscribeNode := &types.UserSubscribeNodeInfo{
				Id:              n.Id,
				Name:            n.Name,
				Uuid:            userSub.UUID,
				Protocol:        n.Protocol,
				Protocols:       server.Protocols,
				Port:            n.Port,
				Address:         n.Address,
				Tags:            strings.Split(n.Tags, ","),
				Country:         server.Country,
				City:            server.City,
				Latitude:        server.Latitude,
				Longitude:       server.Longitude,
				LongitudeCenter: server.LongitudeCenter,
				LatitudeCenter:  server.LatitudeCenter,
				CreatedAt:       n.CreatedAt.Unix(),
			}
			userSubscribeNodes = append(userSubscribeNodes, userSubscribeNode)
		}
	}

	l.Debugf("[Query Subscribe]found servers: %v", len(nodes))
	return userSubscribeNodes, nil
}

// getNodesByGroup gets nodes based on user subscription node_group_id with priority fallback
func (l *QueryUserSubscribeNodeListLogic) getNodesByGroup(userSub *user.Subscribe) ([]*node.Node, error) {
	// 按优先级获取 node_group_id：user_subscribe.node_group_id > subscribe.node_group_id > subscribe.node_group_ids[0]
	nodeGroupId := int64(0)
	source := ""
	var directNodeIds []int64

	// 优先级1: user_subscribe.node_group_id
	if userSub.NodeGroupId != 0 {
		nodeGroupId = userSub.NodeGroupId
		source = "user_subscribe.node_group_id"
	}

	// 获取 subscribe 详情（用于获取 node_group_id 和直接分配的节点）
	subDetails, err := l.svcCtx.SubscribeModel.FindOne(l.ctx, userSub.SubscribeId)
	if err != nil {
		l.Errorw("[GetNodesByGroup] find subscribe details error", logger.Field("error", err.Error()))
		return nil, err
	}

	// 获取直接分配的节点ID
	directNodeIds = tool.StringToInt64Slice(subDetails.Nodes)
	l.Debugf("[GetNodesByGroup] direct nodes: %v", directNodeIds)

	// 如果 user_subscribe 没有 node_group_id，从 subscribe 获取
	if nodeGroupId == 0 {
		// 优先级2: subscribe.node_group_id
		if subDetails.NodeGroupId != 0 {
			nodeGroupId = subDetails.NodeGroupId
			source = "subscribe.node_group_id"
		} else if len(subDetails.NodeGroupIds) > 0 {
			// 优先级3: subscribe.node_group_ids[0]
			nodeGroupId = subDetails.NodeGroupIds[0]
			source = "subscribe.node_group_ids[0]"
		}
	}

	l.Debugf("[GetNodesByGroup] Using %s: %v", source, nodeGroupId)

	// 查询所有启用的节点
	enable := true
	_, allNodes, err := l.svcCtx.NodeModel.FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:    0,
		Size:    10000,
		Enabled: &enable,
	})
	if err != nil {
		l.Errorw("[GetNodesByGroup] FilterNodeList error", logger.Field("error", err.Error()))
		return nil, err
	}

	// 过滤节点
	var resultNodes []*node.Node
	nodeIdMap := make(map[int64]bool)

	for _, n := range allNodes {
		// 1. 公共节点（node_group_ids 为空），所有人可见
		if len(n.NodeGroupIds) == 0 {
			if !nodeIdMap[n.Id] {
				resultNodes = append(resultNodes, n)
				nodeIdMap[n.Id] = true
			}
			continue
		}

		// 2. 如果有节点组，检查节点是否属于该节点组
		if nodeGroupId != 0 {
			for _, gid := range n.NodeGroupIds {
				if gid == nodeGroupId {
					if !nodeIdMap[n.Id] {
						resultNodes = append(resultNodes, n)
						nodeIdMap[n.Id] = true
					}
					break
				}
			}
		}
	}

	// 3. 添加直接分配的节点
	if len(directNodeIds) > 0 {
		for _, n := range allNodes {
			if tool.Contains(directNodeIds, n.Id) && !nodeIdMap[n.Id] {
				resultNodes = append(resultNodes, n)
				nodeIdMap[n.Id] = true
			}
		}
	}

	l.Debugf("[GetNodesByGroup] Found %d nodes (group=%d, direct=%d)", len(resultNodes), nodeGroupId, len(directNodeIds))
	return resultNodes, nil
}

// getNodesByTag gets nodes based on subscribe node_ids and tags
func (l *QueryUserSubscribeNodeListLogic) getNodesByTag(userSub *user.Subscribe) ([]*node.Node, error) {
	subDetails, err := l.svcCtx.SubscribeModel.FindOne(l.ctx, userSub.SubscribeId)
	if err != nil {
		l.Errorw("[Generate Subscribe]find subscribe details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe details error: %v", err.Error())
	}

	nodeIds := tool.StringToInt64Slice(subDetails.Nodes)
	tags := strings.Split(subDetails.NodeTags, ",")
	newTags := make([]string, 0)
	for _, t := range tags {
		if t != "" {
			newTags = append(newTags, t)
		}
	}
	tags = newTags
	l.Debugf("[Generate Subscribe]nodes: %v, NodeTags: %v", nodeIds, tags)

	enable := true
	_, nodes, err := l.svcCtx.NodeModel.FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:    0,
		Size:    1000,
		NodeId:  nodeIds,
		Tag:     tags,
		Enabled: &enable, // Only get enabled nodes
	})

	return nodes, err
}

// getAllNodes returns all enabled nodes
func (l *QueryUserSubscribeNodeListLogic) getAllNodes() ([]*node.Node, error) {
	enable := true
	_, nodes, err := l.svcCtx.NodeModel.FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:    0,
		Size:    1000,
		Enabled: &enable,
	})

	return nodes, err
}

func (l *QueryUserSubscribeNodeListLogic) isSubscriptionExpired(userSub *user.Subscribe) bool {
	return userSub.ExpireTime.Unix() < time.Now().Unix() && userSub.ExpireTime.Unix() != 0
}

func (l *QueryUserSubscribeNodeListLogic) createExpiredServers() []*types.UserSubscribeNodeInfo {
	return nil
}

func (l *QueryUserSubscribeNodeListLogic) getFirstHostLine() string {
	host := l.svcCtx.Config.Host
	lines := strings.Split(host, "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return host
}
func (l *QueryUserSubscribeNodeListLogic) getUserSubscribe(token string) (*user.Subscribe, error) {
	userSub, err := l.svcCtx.UserModel.FindOneSubscribeByToken(l.ctx, token)
	if err != nil {
		l.Infow("[Generate Subscribe]find subscribe error: %v", logger.Field("error", err.Error()), logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}

	//  Ignore expiration check
	//if userSub.Status > 1 {
	//	l.Infow("[Generate Subscribe]subscribe is not available", logger.Field("status", int(userSub.Status)), logger.Field("token", token))
	//	return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe is not available")
	//}

	return userSub, nil
}
