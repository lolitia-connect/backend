package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/ip"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateServerLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreateServerLogic Create Server
func NewCreateServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateServerLogic {
	return &CreateServerLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateServerLogic) CreateServer(req *types.CreateServerRequest) error {
	data := node.Server{
		Name:      req.Name,
		Country:   req.Country,
		City:      req.City,
		Address:   req.Address,
		Sort:      req.Sort,
		Protocols: "",
	}
	protocols := make([]node.Protocol, 0)
	for _, item := range req.Protocols {
		if item.Type == "" {
			return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols type is empty"), "protocols type is empty")
		}
		var protocol node.Protocol
		tool.DeepCopy(&protocol, item)

		if err := applyGeneratedProtocolKeys(&protocol); err != nil {
			l.Logger.Errorf("[CreateServer] Generate Protocol Key Error: %v", err.Error())
			return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "generate protocol key error: %v", err)
		}
		normalized, err := node.NormalizeProtocolForStorage(protocol)
		if err != nil {
			l.Logger.Errorf("[CreateServer] Normalize Protocol Error: %v", err.Error())
			return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocol config is invalid"), "normalize protocol error: %v", err)
		}
		protocols = append(protocols, normalized)
	}

	err := data.MarshalProtocols(protocols)
	if err != nil {
		l.Logger.Errorf("[CreateServer] Marshal Protocols Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols marshal error"), "protocols marshal error: %v", err)
	}
	if data.City == "" && data.Country == "" {
		// query server ip location
		result, err := ip.GetRegionByIp(req.Address)
		if err != nil {
			l.Logger.Errorf("[CreateServer] GetRegionByIp Error: %v", err.Error())
		} else {
			data.City = result.City
			data.Country = result.Country
			data.Latitude = result.Latitude
			data.Longitude = result.Longitude
			data.LatitudeCenter = result.LatitudeCenter
			data.LongitudeCenter = result.LongitudeCenter
		}
	}
	err = l.svcCtx.Store.Node().InsertServer(l.ctx, &data)
	if err != nil {
		l.Logger.Errorf("[CreateServer] Insert Server error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert server error: %v", err)
	}
	return nil
}
