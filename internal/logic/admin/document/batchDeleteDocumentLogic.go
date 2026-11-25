package document

import (
	"context"
	"strconv"

	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type BatchDeleteDocumentLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Batch delete document
func NewBatchDeleteDocumentLogic(ctx context.Context, svcCtx *svc.ServiceContext) *BatchDeleteDocumentLogic {
	return &BatchDeleteDocumentLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *BatchDeleteDocumentLogic) BatchDeleteDocument(req *types.BatchDeleteDocumentRequest) error {
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		for _, id := range req.Ids {
			intID, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				return errors.Wrapf(err, "invalid document id: %v", id)
			}
			if err := store.Document().Delete(l.ctx, intID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		l.Logger.Errorw("[BatchDeleteDocument] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "failed to delete documents: %v", err)
	}
	return nil
}
