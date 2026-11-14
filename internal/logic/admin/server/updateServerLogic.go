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

type UpdateServerLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateServerLogic Update Server
func NewUpdateServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateServerLogic {
	return &UpdateServerLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateServerLogic) UpdateServer(req *types.UpdateServerRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	data, err := nodeStore.FindOneServer(l.ctx, req.Id)
	if err != nil {
		l.Logger.Errorf("[UpdateServer] FindOneServer Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server error: %v", err.Error())
	}
	storedProtocols, err := data.UnmarshalProtocols()
	if err != nil {
		l.Logger.Errorf("[UpdateServer] UnmarshalProtocols Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "unmarshal protocols error: %v", err.Error())
	}
	data.Name = req.Name
	data.Country = req.Country
	data.City = req.City
	// only update address when it's  different
	if req.Address != data.Address || (data.Country == "" || req.Country == "") {
		// query server ip location
		result, err := ip.GetRegionByIp(req.Address)
		if err != nil {
			l.Logger.Errorf("[UpdateServer] GetRegionByIp Error: %v", err.Error())
		} else {
			data.City = result.City
			data.Country = result.Country
			data.Latitude = result.Latitude
			data.Longitude = result.Longitude
			data.LatitudeCenter = result.LatitudeCenter
			data.LongitudeCenter = result.LongitudeCenter
		}
		// update address
		data.Address = req.Address
	}
	protocols := make([]node.Protocol, 0)
	for _, item := range req.Protocols {
		if item.Type == "" {
			return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols type is empty"), "protocols type is empty")
		}
		var protocol node.Protocol
		tool.DeepCopy(&protocol, item)

		if err := applyGeneratedProtocolKeys(&protocol); err != nil {
			l.Logger.Errorf("[UpdateServer] Generate Protocol Key Error: %v", err.Error())
			return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "generate protocol key error: %v", err)
		}
		normalized, err := node.NormalizeProtocolForStorage(protocol)
		if err != nil {
			l.Logger.Errorf("[UpdateServer] Normalize Protocol Error: %v", err.Error())
			return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocol config is invalid"), "normalize protocol error: %v", err)
		}
		// The admin form cannot submit cert_pin_sha256, so the stored value has
		// to survive the edit.
		node.CarryForwardCertPin(storedProtocols, &normalized)
		protocols = append(protocols, normalized)
	}
	err = data.MarshalProtocols(protocols)
	if err != nil {
		l.Logger.Errorf("[UpdateServer] Marshal Protocols Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols marshal error"), "protocols marshal error: %v", err)
	}

	err = nodeStore.UpdateServer(l.ctx, data)
	if err != nil {
		l.Logger.Errorf("[UpdateServer] UpdateServer Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update server error: %v", err.Error())
	}

	return nodeStore.ClearServerCache(l.ctx, req.Id)
}
