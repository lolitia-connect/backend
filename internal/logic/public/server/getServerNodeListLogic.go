package server

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type GetServerNodeListLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetServerNodeListLogic Get all enabled server node list
func NewGetServerNodeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetServerNodeListLogic {
	return &GetServerNodeListLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetServerNodeListLogic) GetServerNodeList() (resp *types.GetServerNodeListResponse, err error) {
	// Query all servers (no pagination, get all)
	_, servers, err := l.svcCtx.Store.Node().FilterServerList(l.ctx, &node.FilterParams{
		Page: 1,
		Size: 1000,
	})
	if err != nil {
		l.Logger.Errorw("[GetServerNodeList] Query Server List Error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query Server List Error")
	}

	list := make([]types.ServerInfo, 0, len(servers))

	for _, server := range servers {
		// Unmarshal protocols
		protocols, err := server.UnmarshalProtocols()
		if err != nil {
			l.Logger.Errorf("[GetServerNodeList] UnmarshalProtocols Error: %s", err.Error())
			continue
		}

		// Build protocol list with ratio
		protocolList := make([]types.ServerNodeProtocol, 0, len(protocols))
		for _, p := range protocols {
			if !p.Enable {
				continue
			}
			ratio := p.Ratio
			if ratio == 0 {
				ratio = 1
			}
			protocolList = append(protocolList, types.ServerNodeProtocol{
				Type:  p.Type,
				Ratio: ratio,
			})
		}

		// Get server status from cache
		var cpu, mem float64
		nodeStatus, err := l.svcCtx.Store.Node().StatusCache(l.ctx, server.Id)
		if err != nil && !errors.Is(err, redis.Nil) {
			l.Logger.Errorw("[GetServerNodeList] Get StatusCache Error: ", zap.Any("error", err.Error()), zap.Any("server_id", server.Id))
		} else {
			cpu = nodeStatus.Cpu
			mem = nodeStatus.Mem
		}

		// Determine online status
		status := getServerStatus(server.LastReportedAt)

		// Count online users
		onlineUsers := l.countOnlineUsers(server.Id, protocols)

		list = append(list, types.ServerInfo{
			Id:          server.Id,
			Name:        server.Name,
			Country:     server.Country,
			City:        server.City,
			Address:     server.Address,
			Status:      status,
			Cpu:         cpu,
			Mem:         mem,
			OnlineUsers: onlineUsers,
			Protocols:   protocolList,
		})
	}

	return &types.GetServerNodeListResponse{
		List: list,
	}, nil
}

// getServerStatus determine server online status based on last reported time
func getServerStatus(last *time.Time) string {
	if last == nil {
		return "offline"
	}
	if time.Since(*last) > 5*time.Minute {
		return "offline"
	}
	if time.Since(*last) > 3*time.Minute {
		return "warning"
	}
	return "online"
}

// countOnlineUsers count unique online users for a server
func (l *GetServerNodeListLogic) countOnlineUsers(serverId int64, protocols []node.Protocol) int64 {
	uniqueUsers := make(map[int64]bool)

	for _, p := range protocols {
		if !p.Enable {
			continue
		}
		data, err := l.svcCtx.Store.Node().OnlineUserSubscribe(l.ctx, serverId, p.Type+":"+p.Id)
		if err != nil {
			if !errors.Is(err, redis.Nil) {
				l.Logger.Errorw("[GetServerNodeList] OnlineUserSubscribe Error: ", zap.Any("error", err.Error()), zap.Any("server_id", serverId), zap.Any("protocol", p.Id))
			}
			continue
		}
		for subscribeId := range data {
			uniqueUsers[subscribeId] = true
		}
	}

	return int64(len(uniqueUsers))
}
