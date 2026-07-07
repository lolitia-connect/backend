package server

import (
	"context"
	"errors"
	"fmt"

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
	if req.ServerId <= 0 || len(req.Users) == 0 {
		return errors.New("invalid request parameters")
	}

	// 验证用户数据
	for _, user := range req.Users {
		if user.SID <= 0 || user.IP == "" {
			return fmt.Errorf("invalid user data: uid=%d, ip=%s", user.SID, user.IP)
		}
	}

	// Find server info
	_, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		l.Logger.Errorw("[PushOnlineUsers] FindOne error", zap.Any("error", err))
		return fmt.Errorf("server not found: %w", err)
	}

	onlineUsers := make(node.OnlineUserSubscribe)
	for _, user := range req.Users {
		if online, ok := onlineUsers[user.SID]; ok {
			// If user already exists, update IP if different
			online = append(online, user.IP)
			onlineUsers[user.SID] = online
		} else {
			// New user, add to map
			onlineUsers[user.SID] = []string{user.IP}
		}
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

	return nil
}
