package console

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type QueryTicketWaitReplyLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryTicketWaitReplyLogic Query ticket wait reply
func NewQueryTicketWaitReplyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryTicketWaitReplyLogic {
	return &QueryTicketWaitReplyLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryTicketWaitReplyLogic) QueryTicketWaitReply() (resp *types.TicketWaitRelpyResponse, err error) {
	count, err := l.svcCtx.Store.Ticket().QueryWaitReplyTotal(l.ctx)
	if err != nil {
		l.Logger.Errorw("[QueryTicketWaitReply] Query Database Error: ", zap.Any("error", err.Error()))
		return nil, err
	}
	return &types.TicketWaitRelpyResponse{
		Count: count,
	}, nil
}
