package announcement

import (
	"context"

	model "github.com/perfect-panel/server/internal/model/announcement"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateAnnouncementLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update announcement
func NewUpdateAnnouncementLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateAnnouncementLogic {
	return &UpdateAnnouncementLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateAnnouncementLogic) UpdateAnnouncement(req *types.UpdateAnnouncementRequest) error {
	info, err := l.svcCtx.Store.Announcement().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorw("[UpdateAnnouncement] Query Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get announcement error: %v", err.Error())
	}
	data := &model.Announcement{Id: req.Id, Title: req.Title, Content: req.Content, Show: info.Show, Pinned: info.Pinned, Popup: info.Popup}
	if req.Show != nil {
		data.Show = req.Show
	}
	if req.Pinned != nil {
		data.Pinned = req.Pinned
	}
	if req.Popup != nil {
		data.Popup = req.Popup
	}
	if err := l.svcCtx.Store.Announcement().Update(l.ctx, data); err != nil {
		l.Logger.Errorw("[UpdateAnnouncement] Update Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update announcement error: %v", err.Error())
	}
	return nil
}
