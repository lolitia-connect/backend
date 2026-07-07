package subscribe

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/pkg/device"

	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type UpdateSubscribeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Update subscribe
func NewUpdateSubscribeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSubscribeLogic {
	return &UpdateSubscribeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateSubscribeLogic) UpdateSubscribe(req *types.UpdateSubscribeRequest) error {
	// Query the database to get the subscribe information
	_, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Error("[UpdateSubscribe] Database query error", zap.Any("error", err.Error()), zap.Any("subscribe_id", req.Id))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe error: %v", err.Error())
	}
	discount := ""
	if len(req.Discount) > 0 {
		val, _ := json.Marshal(req.Discount)
		discount = string(val)
	}
	// When NodeTags is set, clear Nodes to avoid AND-combined query returning wrong results (#94)
	nodes := tool.Int64SliceToString(req.Nodes.Int64s())
	if len(req.NodeTags) > 0 {
		nodes = ""
	}
	sub := &subscribe.Subscribe{
		Id:               req.Id,
		Name:             req.Name,
		Language:         req.Language,
		Description:      req.Description,
		UnitPrice:        req.UnitPrice,
		UnitTime:         req.UnitTime,
		Discount:         discount,
		Replacement:      req.Replacement,
		Inventory:        req.Inventory,
		Traffic:          req.Traffic,
		TrafficUnlimited: req.TrafficUnlimited,
		SpeedLimit:       req.SpeedLimit,
		DeviceLimit:      req.DeviceLimit,
		Quota:            req.Quota,
		Nodes:            nodes,
		NodeTags:         tool.StringSliceToString(req.NodeTags),
		NodeGroupIds:     subscribe.JSONInt64Slice(tool.StringSliceToInt64Slice(req.NodeGroupIds)),
		NodeGroupId:      req.NodeGroupId,
		TrafficLimit: func() string {
			if len(req.TrafficLimit) > 0 {
				val, _ := json.Marshal(req.TrafficLimit)
				return string(val)
			}
			return ""
		}(),
		Show:              req.Show,
		Sell:              req.Sell,
		Sort:              req.Sort,
		DeductionRatio:    req.DeductionRatio,
		AllowDeduction:    req.AllowDeduction,
		ResetCycle:        req.ResetCycle,
		RenewalReset:      req.RenewalReset,
		ShowOriginalPrice: req.ShowOriginalPrice,
	}
	err = l.svcCtx.Store.Subscribe().Update(l.ctx, sub)
	if err != nil {
		l.Logger.Error("[UpdateSubscribe] update subscribe failed", zap.Any("error", err.Error()), zap.Any("subscribe", sub))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update subscribe error: %v", err.Error())
	}
	l.svcCtx.DeviceManager.Broadcast(device.SubscribeUpdate)
	return nil
}
