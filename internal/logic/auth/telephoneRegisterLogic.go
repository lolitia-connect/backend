package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
)

type TelephoneRegisterLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// User Telephone register
func NewTelephoneRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TelephoneRegisterLogic {
	return &TelephoneRegisterLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TelephoneRegisterLogic) TelephoneRegister(req *types.TelephoneRegisterRequest) (resp *types.LoginResponse, err error) {
	// todo: add your logic here and delete this line

	return
}
