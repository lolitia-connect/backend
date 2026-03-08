package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/subscribe"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
)

type GetServerUserListLogic struct {
	logger.Logger
	ctx    *gin.Context
	svcCtx *svc.ServiceContext
}

// NewGetServerUserListLogic Get user list
func NewGetServerUserListLogic(ctx *gin.Context, svcCtx *svc.ServiceContext) *GetServerUserListLogic {
	return &GetServerUserListLogic{
		Logger: logger.WithContext(ctx.Request.Context()),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetServerUserListLogic) GetServerUserList(req *types.GetServerUserListRequest) (resp *types.GetServerUserListResponse, err error) {
	cacheKey := fmt.Sprintf("%s%d", node.ServerUserListCacheKey, req.ServerId)
	cache, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if cache != "" {
		etag := tool.GenerateETag([]byte(cache))
		resp = &types.GetServerUserListResponse{}
		//  Check If-None-Match header
		if match := l.ctx.GetHeader("If-None-Match"); match == etag {
			return nil, xerr.StatusNotModified
		}
		l.ctx.Header("ETag", etag)
		err = json.Unmarshal([]byte(cache), resp)
		if err != nil {
			l.Errorw("[ServerUserListCacheKey] json unmarshal error", logger.Field("error", err.Error()))
			return nil, err
		}
		return resp, nil
	}
	server, err := l.svcCtx.NodeModel.FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		return nil, err
	}

	// 查询该服务器上该协议的所有节点（包括属于节点组的节点）
	_, nodes, err := l.svcCtx.NodeModel.FilterNodeList(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{server.Id},
		Protocol: req.Protocol,
	})
	if err != nil {
		l.Errorw("FilterNodeList error", logger.Field("error", err.Error()))
		return nil, err
	}

	if len(nodes) == 0 {
		return &types.GetServerUserListResponse{
			Users: []types.ServerUser{
				{
					Id:   1,
					UUID: uuidx.NewUUID().String(),
				},
			},
		}, nil
	}

	// 收集所有唯一的节点组 ID
	nodeGroupMap := make(map[int64]bool) // nodeGroupId -> true
	var nodeIds []int64
	var nodeTags []string

	for _, n := range nodes {
		nodeIds = append(nodeIds, n.Id)
		if n.Tags != "" {
			nodeTags = append(nodeTags, strings.Split(n.Tags, ",")...)
		}
		// 收集节点组 ID
		if len(n.NodeGroupIds) > 0 {
			for _, gid := range n.NodeGroupIds {
				if gid > 0 {
					nodeGroupMap[gid] = true
				}
			}
		}
	}

	// 获取所有节点组 ID
	nodeGroupIds := make([]int64, 0, len(nodeGroupMap))
	for gid := range nodeGroupMap {
		nodeGroupIds = append(nodeGroupIds, gid)
	}

	// 查询订阅：
	// 1. 如果有节点组，查询匹配这些节点组的订阅
	// 2. 如果没有节点组，查询使用节点 ID 或 tags 的订阅
	var subs []*subscribe.Subscribe
	if len(nodeGroupIds) > 0 {
		// 节点组模式：查询 node_group_id 或 node_group_ids 匹配的订阅
		_, subs, err = l.svcCtx.SubscribeModel.FilterListByNodeGroups(l.ctx, &subscribe.FilterByNodeGroupsParams{
			Page:         1,
			Size:         9999,
			NodeGroupIds: nodeGroupIds,
		})
		if err != nil {
			l.Errorw("FilterListByNodeGroups error", logger.Field("error", err.Error()))
			return nil, err
		}
	} else {
		// 传统模式：查询匹配节点 ID 或 tags 的订阅
		nodeTags = tool.RemoveDuplicateElements(nodeTags...)
		_, subs, err = l.svcCtx.SubscribeModel.FilterList(l.ctx, &subscribe.FilterParams{
			Page: 1,
			Size: 9999,
			Node: nodeIds,
			Tags: nodeTags,
		})
		if err != nil {
			l.Errorw("FilterList error", logger.Field("error", err.Error()))
			return nil, err
		}
	}

	if len(subs) == 0 {
		return &types.GetServerUserListResponse{
			Users: []types.ServerUser{
				{
					Id:   1,
					UUID: uuidx.NewUUID().String(),
				},
			},
		}, nil
	}
	users := make([]types.ServerUser, 0)
	for _, sub := range subs {
		data, err := l.svcCtx.UserModel.FindUsersSubscribeBySubscribeId(l.ctx, sub.Id)
		if err != nil {
			return nil, err
		}
		for _, datum := range data {
			users = append(users, types.ServerUser{
				Id:          datum.Id,
				UUID:        datum.UUID,
				SpeedLimit:  sub.SpeedLimit,
				DeviceLimit: sub.DeviceLimit,
			})
		}
	}
	if len(users) == 0 {
		users = append(users, types.ServerUser{
			Id:   1,
			UUID: uuidx.NewUUID().String(),
		})
	}
	resp = &types.GetServerUserListResponse{
		Users: users,
	}
	val, _ := json.Marshal(resp)
	etag := tool.GenerateETag(val)
	l.ctx.Header("ETag", etag)
	err = l.svcCtx.Redis.Set(l.ctx, cacheKey, string(val), -1).Err()
	if err != nil {
		l.Errorw("[ServerUserListCacheKey] redis set error", logger.Field("error", err.Error()))
	}
	//  Check If-None-Match header
	if match := l.ctx.GetHeader("If-None-Match"); match == etag {
		return nil, xerr.StatusNotModified
	}
	return resp, nil
}
