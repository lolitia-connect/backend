package user

import (
	"context"
	"os"
	"strings"

	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BatchDeleteUserLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	Logger *zap.SugaredLogger
}

func NewBatchDeleteUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteUserLogic {
	return &BatchDeleteUserLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: zap.S(),
	}
}

func (l *BatchDeleteUserLogic) BatchDeleteUser(req *types.BatchDeleteUserRequest) error {
	isDemo := strings.ToLower(os.Getenv("PPANEL_MODE")) == "demo"
	ids := tool.StringSliceToInt64Slice(req.Ids)

	if tool.Contains(ids, int64(2)) && isDemo {
		return errors.Wrapf(xerr.NewErrCodeMsg(503, "Demo mode does not allow deletion of the admin user"), "BatchDeleteUser failed: cannot delete admin user in demo mode")
	}

	err := l.svcCtx.Store.User().BatchDeleteUser(l.ctx, ids)
	if err != nil {
		l.Logger.Error("[BatchDeleteUserLogic] BatchDeleteUser failed: ", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "BatchDeleteUser failed: %v", err.Error())
	}
	return nil
}
