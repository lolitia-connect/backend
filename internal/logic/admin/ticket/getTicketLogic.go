package ticket

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type GetTicketLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get ticket detail
func NewGetTicketLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTicketLogic {
	return &GetTicketLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetTicketLogic) GetTicket(req *types.GetTicketRequest) (resp *types.Ticket, err error) {
	data, err := l.svcCtx.Store.Ticket().QueryTicketDetail(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[GetTicket] Query Database Error: ", zap.Any("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get ticket detail failed: %v", err.Error())
	}
	resp = &types.Ticket{}
	tool.DeepCopy(resp, data)
	return
}
