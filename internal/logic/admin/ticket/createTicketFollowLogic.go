package ticket

import (
	"context"

	"github.com/perfect-panel/server/internal/model/ticket"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateTicketFollowLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create ticket follow
func NewCreateTicketFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTicketFollowLogic {
	return &CreateTicketFollowLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateTicketFollowLogic) CreateTicketFollow(req *types.CreateTicketFollowRequest) (err error) {
	// find ticket
	_, err = l.svcCtx.Store.Ticket().FindOne(l.ctx, req.TicketId)
	if err != nil {
		l.Logger.Errorw("[CreateTicketFollow] FindOne error", zap.Any("error", err.Error()), zap.Any("ticketId", req.TicketId))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find ticket failed: %v", err.Error())
	}
	err = l.svcCtx.Store.Ticket().InsertTicketFollow(l.ctx, &ticket.Follow{
		TicketId: req.TicketId,
		From:     req.From,
		Type:     req.Type,
		Content:  req.Content,
	})
	if err != nil {
		l.Logger.Errorw("[CreateTicketFollow] Database insert error", zap.Any("error", err.Error()), zap.Any("request", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create ticket follow failed: %v", err.Error())
	}
	err = l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, req.TicketId, 0, ticket.Waiting)
	if err != nil {
		l.Logger.Errorw("[CreateTicketFollow] Database update error", zap.Any("error", err.Error()), zap.Any("status", ticket.Waiting))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ticket status failed: %v", err.Error())
	}
	return
}
