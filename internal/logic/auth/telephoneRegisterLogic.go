package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"go.uber.org/zap"
)

type TelephoneRegisterLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// User Telephone register
func NewTelephoneRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TelephoneRegisterLogic {
	return &TelephoneRegisterLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TelephoneRegisterLogic) TelephoneRegister(req *types.TelephoneRegisterRequest) (*types.LoginResponse, error) {
	return NewTelephoneUserRegisterLogic(l.ctx, l.svcCtx).TelephoneUserRegister(req)
}
