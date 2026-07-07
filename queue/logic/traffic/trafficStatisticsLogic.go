package traffic

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"go.uber.org/zap"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/traffic"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/queue/types"
)

//goland:noinspection GoNameStartsWithPackageName
type TrafficStatisticsLogic struct {
	svc *svc.ServiceContext
}

func NewTrafficStatisticsLogic(svc *svc.ServiceContext) *TrafficStatisticsLogic {
	return &TrafficStatisticsLogic{
		svc: svc,
	}
}

func (l *TrafficStatisticsLogic) ProcessTask(ctx context.Context, task *asynq.Task) error {
	var payload types.TrafficStatistics
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		zap.S().Error("[TrafficStatistics] Unmarshal payload failed",
			zap.Any("error", err.Error()),
			zap.Any("payload", string(task.Payload())),
		)
		return nil
	}
	if len(payload.Logs) == 0 {
		zap.S().Error("[TrafficStatistics] Payload is empty")
		return nil
	}
	// query server info
	serverInfo, err := l.svc.Store.Node().FindOneServer(ctx, payload.ServerId)
	if err != nil {
		zap.S().Error("[TrafficStatistics] Find server info failed",
			zap.Any("serverId", payload.ServerId),
			zap.Any("error", err.Error()),
		)
		return nil
	}
	// query protocol ratio
	// default ratio is 1.0

	protocols, err := serverInfo.UnmarshalProtocols()
	if err != nil {
		zap.S().Errorf("[TrafficStatistics] Unmarshal protocols failed: %s", err.Error())
		return nil
	}
	var protocol *node.Protocol

	var ratio float32 = 1.0

	for _, p := range protocols {
		if p.Id == payload.ProtocolId && p.Type == payload.Protocol {
			protocol = &p
			break
		}
	}

	if protocol == nil {
		zap.S().Error("[TrafficStatistics] Protocol not found",
			zap.Any("server_id", payload.ServerId),
			zap.Any("protocol", payload.Protocol),
			zap.Any("protocol_id", payload.ProtocolId),
		)
		return nil
	}

	// use protocol ratio if it's greater than 0
	if protocol.Ratio > 0 {
		ratio = float32(protocol.Ratio)
	}

	now := time.Now()
	realTimeMultiplier := l.svc.NodeMultiplierManager.GetMultiplier(now)
	zap.S().Debugf("[TrafficStatisticsLogic] Current time traffic multiplier: %.2f", realTimeMultiplier)
	for _, log := range payload.Logs {
		// query user Subscribe Info
		sub, err := l.svc.Store.User().FindOneSubscribe(ctx, log.SID)
		if err != nil {
			zap.S().Error("[TrafficStatistics] Find user Subscribe Info failed",
				zap.Any("uid", log.SID),
				zap.Any("error", err.Error()),
			)
			continue
		}

		if log.Download+log.Upload <= l.svc.Config.Node.TrafficReportThreshold {
			// no traffic, skip
			continue
		}
		// update user subscribe with log
		d := int64(float32(log.Download) * ratio * realTimeMultiplier)
		u := int64(float32(log.Upload) * ratio * realTimeMultiplier)
		isExpired := sub.Status == 3 // Status 3 = Expired

		// Update user subscribe traffic using ent model layer.
		// The FindOneSubscribe call has been removed from inside the method
		// to avoid PXC's SELECT→UPDATE row-level conflict (Error 1020).
		if err := l.svc.Store.User().UpdateUserSubscribeWithTraffic(ctx, sub.Id, d, u, isExpired); err != nil {
			zap.S().Error("[TrafficStatistics] Update user subscribe with log failed",
				zap.Any("uid", log.SID),
				zap.Any("download", float32(log.Download)*ratio),
				zap.Any("upload", float32(log.Upload)*ratio),
				zap.Any("is_expired", isExpired),
				zap.Any("error", err.Error()),
			)
			continue
		}

		// Insert traffic log record
		if err := l.svc.Store.TrafficLog().Insert(ctx, &traffic.TrafficLog{
			ServerId:    payload.ServerId,
			SubscribeId: log.SID,
			UserId:      sub.UserId,
			Upload:      u,
			Download:    d,
			Timestamp:   now,
		}); err != nil {
			zap.S().Error("[TrafficStatistics] Insert traffic log failed",
				zap.Any("uid", log.SID),
				zap.Any("download", float32(log.Download)*ratio),
				zap.Any("upload", float32(log.Upload)*ratio),
				zap.Any("error", err.Error()),
			)
		}
	}
	return nil
}
