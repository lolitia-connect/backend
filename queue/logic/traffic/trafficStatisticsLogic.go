package traffic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	serverlogic "github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/trafficagg"
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
		return fmt.Errorf("invalid traffic task: %w", asynq.SkipRetry)
	}
	serverInfo, err := l.svc.Store.Node().FindOneServer(ctx, payload.ServerId)
	if err != nil {
		return err
	}
	logs := make([]trafficagg.Report, 0, len(payload.Logs))
	for _, entry := range payload.Logs {
		logs = append(logs, trafficagg.Report{SID: entry.SID, Upload: entry.Upload, Download: entry.Download})
	}
	taskID, ok := asynq.GetTaskID(ctx)
	if !ok {
		taskID = string(task.Payload())
	}
	digest := sha256.Sum256([]byte(taskID))
	return serverlogic.NewTrafficAggregator(l.svc).AddReportOnceAt(ctx, serverInfo, payload.Protocol, payload.ProtocolId, logs, time.Now(), hex.EncodeToString(digest[:]))
}

type FlushTrafficLogic struct {
	svc *svc.ServiceContext
}

func NewFlushTrafficLogic(service *svc.ServiceContext) *FlushTrafficLogic {
	return &FlushTrafficLogic{svc: service}
}

func (logic *FlushTrafficLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	return (&trafficagg.Aggregator{Store: logic.svc.Store, Redis: logic.svc.Redis}).Flush(ctx, time.Now())
}
