package subscribe

import (
	"context"
	"strings"
	"time"

	entnodegroup "github.com/perfect-panel/server/ent/nodegroup"
	"github.com/perfect-panel/server/internal/model/group"
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

	userSubscribes, err := l.svcCtx.Store.User().QueryUserSubscribe(l.ctx, u.Id, 1, 2)
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
			Id:               userSubscribe.Id,
			Nodes:            nodes,
			Traffic:          userSubscribe.Traffic,
			TrafficUnlimited: userSubscribe.TrafficUnlimited,
			Upload:           userSubscribe.Upload,
			Download:         userSubscribe.Download,
			Token:            userSubscribe.Token,
			UserId:           userSubscribe.UserId,
			OrderId:          userSubscribe.OrderId,
			SubscribeId:      userSubscribe.SubscribeId,
			StartTime:        userSubscribe.StartTime.Unix(),
			ExpireTime:       userSubscribe.ExpireTime.Unix(),
			Status:           userSubscribe.Status,
			CreatedAt:        userSubscribe.CreatedAt.Unix(),
			UpdatedAt:        userSubscribe.UpdatedAt.Unix(),
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
	if l.isSubscriptionExpired(userSub) || l.isTrafficExhausted(userSub) {
		nodes, err := l.createExpiredServers(userSub)
		if err != nil {
			return nil, err
		}
		return nodes, nil
	}

	subDetails, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, userSub.SubscribeId)
	if err != nil {
		l.Debugw("[GetServers] Failed to check group enabled", logger.Field("error", err.Error()))
		// Continue with tag-based filtering
	}
	nodeIds := tool.StringToInt64Slice(subDetails.Nodes)
	tags := strings.Split(subDetails.NodeTags, ",")
	cleanTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
	}
	tags = cleanTags

	enable := true

	nodes, err := l.filterSubscribeNodes(nodeIds, tags, enable)
	if err != nil {
		l.Errorw("[Generate Subscribe]find server details error: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server details error: %v", err.Error())
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

		servers, err := l.svcCtx.Store.Node().QueryServerList(l.ctx, serverIds)
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
	logger.Debugf("[Generate Subscribe]found servers: %v", len(nodes))
	return userSubscribeNodes, nil
}

func (l *QueryUserSubscribeNodeListLogic) filterSubscribeNodes(nodeIds []int64, tags []string, enable bool) ([]*node.Node, error) {
	addNodes := func(result []*node.Node, seen map[int64]struct{}, items []*node.Node) []*node.Node {
		for _, item := range items {
			if item == nil {
				continue
			}
			if _, ok := seen[item.Id]; ok {
				continue
			}
			seen[item.Id] = struct{}{}
			result = append(result, item)
		}
		return result
	}

	if len(nodeIds) == 0 && len(tags) == 0 {
		_, nodes, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
			Page:    0,
			Size:    1000,
			Enabled: &enable,
		})
		return nodes, err
	}

	seen := make(map[int64]struct{})
	nodes := make([]*node.Node, 0)
	if len(nodeIds) > 0 {
		_, directNodes, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
			Page:    0,
			Size:    1000,
			NodeId:  nodeIds,
			Enabled: &enable,
		})
		if err != nil {
			return nil, err
		}
		nodes = addNodes(nodes, seen, directNodes)
	}
	if len(tags) > 0 {
		_, tagNodes, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
			Page:    0,
			Size:    1000,
			Tag:     tags,
			Enabled: &enable,
		})
		if err != nil {
			return nil, err
		}
		nodes = addNodes(nodes, seen, tagNodes)
	}
	return nodes, nil
}

func (l *QueryUserSubscribeNodeListLogic) isSubscriptionExpired(userSub *user.Subscribe) bool {
	return userSub.ExpireTime.Unix() < time.Now().Unix() && userSub.ExpireTime.Unix() != 0
}

// isTrafficExhausted reports whether the subscription has used up its traffic
// quota (Traffic == 0 means unlimited).
func (l *QueryUserSubscribeNodeListLogic) isTrafficExhausted(userSub *user.Subscribe) bool {
	return userSub.Traffic > 0 && userSub.Download+userSub.Upload >= userSub.Traffic
}

func (l *QueryUserSubscribeNodeListLogic) createExpiredServers(userSub *user.Subscribe) ([]*types.UserSubscribeNodeInfo, error) {
	// 1. 查询过期节点组
	expiredGroup, err := l.svcCtx.Ent.NodeGroup.Query().Where(entnodegroup.IsExpiredGroup(true)).First(l.ctx)
	if err != nil {
		l.Debugw("no expired node group configured", logger.Field("error", err))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
	}
	if !group.IsNodeGroupTypeAccessible(expiredGroup.Type, group.NodeGroupAccessApp) {
		l.Debugf("expired node group %d is not accessible for app output", expiredGroup.ID)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
	}

	// 2. 检查用户是否在过期天数限制内
	expiredDays := int(time.Since(userSub.ExpireTime).Hours() / 24)
	if expiredDays > expiredGroup.ExpiredDaysLimit {
		l.Debugf("user subscription expired %d days, exceeds limit %d days", expiredDays, expiredGroup.ExpiredDaysLimit)
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
	}

	// 3. 检查用户已使用流量是否超过限制(仅使用过期期间的流量)
	if expiredGroup.MaxTrafficGBExpired != nil && *expiredGroup.MaxTrafficGBExpired > 0 {
		usedTrafficGB := (userSub.ExpiredDownload + userSub.ExpiredUpload) / (1024 * 1024 * 1024)
		if usedTrafficGB >= *expiredGroup.MaxTrafficGBExpired {
			l.Debugf("user expired traffic %d GB, exceeds expired group limit %d GB", usedTrafficGB, *expiredGroup.MaxTrafficGBExpired)
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
		}
	}

	// 4. 查询过期节点组的节点
	enable := true
	isHidden := false
	_, nodes, err := l.svcCtx.Store.Node().FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:         0,
		Size:         1000,
		NodeGroupIds: []int64{expiredGroup.ID},
		Enabled:      &enable,
		IsHidden:     &isHidden,
	})
	if err != nil {
		l.Errorw("failed to query expired group nodes", logger.Field("error", err))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
	}

	if len(nodes) == 0 {
		l.Debug("no nodes found in expired group")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
	}

	// 5. 查询服务器信息
	var serverMapIds = make(map[int64]*node.Server)
	for _, n := range nodes {
		serverMapIds[n.ServerId] = nil
	}
	var serverIds []int64
	for k := range serverMapIds {
		serverIds = append(serverIds, k)
	}

	servers, err := l.svcCtx.Store.Node().QueryServerList(l.ctx, serverIds)
	if err != nil {
		l.Errorw("failed to query servers", logger.Field("error", err))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeExpired), "subscribe is expired")
	}

	for _, s := range servers {
		serverMapIds[s.Id] = s
	}

	// 6. 构建节点列表
	userSubscribeNodes := make([]*types.UserSubscribeNodeInfo, 0, len(nodes))
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

	l.Infof("returned %d nodes from expired group for user %d (expired %d days)", len(userSubscribeNodes), userSub.UserId, expiredDays)
	return userSubscribeNodes, nil
}

func (l *QueryUserSubscribeNodeListLogic) getAccessibleNodeGroup(nodeGroupId int64, accessType string) *group.NodeGroup {
	if nodeGroupId == 0 {
		return nil
	}

	entNodeGroup, err := l.svcCtx.Ent.NodeGroup.Query().Where(entnodegroup.ID(nodeGroupId)).Only(l.ctx)
	if err != nil {
		l.Debugw("[GetNodesByGroup] node group not found", logger.Field("nodeGroupId", nodeGroupId), logger.Field("error", err.Error()))
		return nil
	}

	if !group.IsNodeGroupTypeAccessible(entNodeGroup.Type, accessType) {
		return nil
	}

	nodeGroup := group.NodeGroup{Id: entNodeGroup.ID, Type: entNodeGroup.Type}
	nodeGroup.Type = group.MustNodeGroupType(nodeGroup.Type)
	return &nodeGroup
}

func (l *QueryUserSubscribeNodeListLogic) getUserSubscribe(token string) (*user.Subscribe, error) {
	userSub, err := l.svcCtx.Store.User().FindOneSubscribeByToken(l.ctx, token)
	if err != nil {
		l.Infow("[Generate Subscribe]find subscribe error: %v", logger.Field("error", err.Error()), logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe error: %v", err.Error())
	}
	if userSub == nil {
		l.Infow("[Generate Subscribe]subscribe token not found", logger.Field("token", token))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe token not found")
	}

	//  Ignore expiration check
	//if userSub.Status > 1 {
	//	l.Infow("[Generate Subscribe]subscribe is not available", logger.Field("status", int(userSub.Status)), logger.Field("token", token))
	//	return nil, errors.Wrapf(xerr.NewErrCode(xerr.SubscribeNotAvailable), "subscribe is not available")
	//}

	return userSub, nil
}
