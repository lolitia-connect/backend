package auth

import (
	"context"

	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

func clearTrialSubscribeCache(ctx context.Context, svcCtx *svc.ServiceContext, trialSub *user.Subscribe) {
	if trialSub == nil {
		return
	}
	if err := svcCtx.Store.Subscribe().ClearCache(ctx, trialSub.SubscribeId); err != nil {
		zap.S().Errorw("Clear subscribe cache failed",
			zap.Any("error", err.Error()),
			zap.Any("subscribe_id", trialSub.SubscribeId),
		)
	}
}
