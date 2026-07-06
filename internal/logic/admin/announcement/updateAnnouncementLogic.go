package announcement

import (
	"context"

	entannouncement "github.com/perfect-panel/server/ent/announcement"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateAnnouncementLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update announcement
func NewUpdateAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAnnouncementLogic {
	return &UpdateAnnouncementLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAnnouncementLogic) UpdateAnnouncement(req *types.UpdateAnnouncementRequest) error {
	_, err := l.svcCtx.Ent.Announcement.Query().Where(entannouncement.ID(req.Id)).Only(l.ctx)
	if err != nil {
		l.Errorw("[UpdateAnnouncement] Query Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get announcement error: %v", err.Error())
	}
	update := l.svcCtx.Ent.Announcement.UpdateOneID(req.Id).
		SetTitle(req.Title).
		SetContent(req.Content)
	if req.Show != nil {
		update.SetShow(*req.Show)
	}
	if req.Pinned != nil {
		update.SetPinned(*req.Pinned)
	}
	if req.Popup != nil {
		update.SetPopup(*req.Popup)
	}
	err = update.Exec(l.ctx)
	if err != nil {
		l.Errorw("[UpdateAnnouncement] Update Database Error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update announcement error: %v", err.Error())
	}
	return nil
}
