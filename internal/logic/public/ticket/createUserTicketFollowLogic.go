package ticket

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/ticket"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateUserTicketFollowLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create ticket follow
func NewCreateUserTicketFollowLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserTicketFollowLogic {
	return &CreateUserTicketFollowLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateUserTicketFollowLogic) CreateUserTicketFollow(req *types.CreateUserTicketFollowRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		zap.S().Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	// query ticket
	t, err := l.svcCtx.Store.Ticket().FindOne(l.ctx, req.TicketId)
	if err != nil {
		l.Logger.Errorw("[CreateUserTicketFollow] Database query error", zap.Any("error", err.Error()), zap.Any("request", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query ticket failed: %v", err.Error())
	}
	// check access
	if u.Id != t.UserId {
		l.Logger.Errorw("[CreateUserTicketFollow] Invalid access", zap.Any("user_id", u.Id), zap.Any("ticket_user_id", t.UserId))
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "invalid access")
	}
	// insert follow
	err = l.svcCtx.Store.Ticket().InsertTicketFollow(l.ctx, &ticket.Follow{
		TicketId: req.TicketId,
		From:     req.From,
		Type:     req.Type,
		Content:  req.Content,
	})
	if err != nil {
		l.Logger.Errorw("[CreateUserTicketFollow] Database insert error", zap.Any("error", err.Error()), zap.Any("request", req))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create ticket follow failed: %v", err.Error())
	}
	err = l.svcCtx.Store.Ticket().UpdateTicketStatus(l.ctx, req.TicketId, u.Id, ticket.Pending)
	if err != nil {
		l.Logger.Errorw("[CreateUserTicketFollow] Database update error", zap.Any("error", err.Error()), zap.Any("status", ticket.Pending))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update ticket status failed: %v", err.Error())
	}
	return nil
}
