package server

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type PushOnlineUsersLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewPushOnlineUsersLogic Push online users
func NewPushOnlineUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PushOnlineUsersLogic {
	return &PushOnlineUsersLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PushOnlineUsersLogic) PushOnlineUsers(req *types.OnlineUsersRequest) error {
	// 验证请求数据
	if req.ServerId <= 0 {
		return errors.New("invalid request parameters")
	}

	// 验证用户数据
	for _, user := range req.Users {
		address, err := netip.ParseAddr(user.IP)
		if user.SID <= 0 || err != nil || address.Zone() != "" || address.IsUnspecified() {
			return fmt.Errorf("invalid user data: uid=%d, ip=%s", user.SID, user.IP)
		}
	}

	served, err := ServedSubscriptionIDs(l.ctx, l.svcCtx, req.ServerId, req.Protocol, req.ProtocolId)
	if err != nil {
		return err
	}

	onlineUsers := make(node.OnlineUserSubscribe)
	seen := make(map[types.OnlineUser]struct{}, len(req.Users))
	for _, user := range req.Users {
		if _, ok := served[user.SID]; !ok {
			continue
		}
		address, _ := netip.ParseAddr(user.IP)
		user.IP = address.Unmap().String()
		if _, ok := seen[user]; ok {
			continue
		}
		seen[user] = struct{}{}
		onlineUsers[user.SID] = append(onlineUsers[user.SID], user.IP)
	}
	err = l.svcCtx.Store.Node().UpdateOnlineUserSubscribe(l.ctx, req.ServerId, fmt.Sprintf("%s:%s", req.Protocol, req.ProtocolId), onlineUsers)
	if err != nil {
		l.Logger.Errorw("[PushOnlineUsers] cache operation error", zap.Any("error", err))
		return err
	}

	err = l.svcCtx.Store.Node().UpdateOnlineUserSubscribeGlobal(l.ctx, onlineUsers)

	if err != nil {
		l.Logger.Errorw("[PushOnlineUsers] cache operation error", zap.Any("error", err))
		return err
	}

	return node.RenewDeviceLeases(l.ctx, l.svcCtx.Redis, onlineUsers)
}
