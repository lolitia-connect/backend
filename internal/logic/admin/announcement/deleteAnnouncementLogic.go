package announcement

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeleteAnnouncementLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete announcement
func NewDeleteAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteAnnouncementLogic {
	return &DeleteAnnouncementLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeleteAnnouncementLogic) DeleteAnnouncement(req *types.DeleteAnnouncementRequest) error {
	if err := l.svcCtx.Ent.Announcement.DeleteOneID(req.Id).Exec(l.ctx); err != nil && !ent.IsNotFound(err) {
		l.Errorw("[DeleteAnnouncement] Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete announcement failed: %v", err.Error())
	}
	return nil
}
