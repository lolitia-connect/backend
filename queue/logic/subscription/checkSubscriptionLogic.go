package subscription

import (
	"context"
	"encoding/json"
	"time"

	queue "github.com/perfect-panel/server/queue/types"

	"go.uber.org/zap"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
)

type CheckSubscriptionLogic struct {
	svc *svc.ServiceContext
}

func NewCheckSubscriptionLogic(svc *svc.ServiceContext) *CheckSubscriptionLogic {
	return &CheckSubscriptionLogic{
		svc: svc,
	}
}

func (l *CheckSubscriptionLogic) ProcessTask(ctx context.Context, _ *asynq.Task) error {
	zap.S().Infof("[CheckSubscription] Start check subscription: %s", time.Now().Format("2006-01-02 15:04:05"))
	// Check subscription traffic
	err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
		list, err := store.User().FindTrafficExceededSubscribes(ctx)
		if err != nil {
			zap.S().Errorw("[Check Subscription Traffic] Query subscribe failed", zap.Any("error", err.Error()))
			return err
		}
		var ids []int64
		for _, item := range list {
			ids = append(ids, item.Id)
		}
		if len(ids) > 0 {
			if err = store.User().MarkSubscribesFinished(ctx, ids, 2, time.Now()); err != nil {
				zap.S().Errorw("[Check Subscription Traffic] Update subscribe status failed", zap.Any("error", err.Error()))
				return nil
			}
			err = l.sendTrafficNotify(ctx, ids)
			if err != nil {
				zap.S().Errorw("[Check Subscription Traffic] Send email failed", zap.Any("error", err.Error()))
				return nil
			}

			l.clearServerCache(ctx, list...)
			zap.S().Infow("[Check Subscription Traffic] Update subscribe status", zap.Any("user_ids", ids), zap.Any("count", int64(len(ids))))
		} else {
			zap.S().Info("[Check Subscription Traffic] No subscribe need to update")
		}

		return nil
	})
	if err != nil {
		zap.S().Error("[CheckSubscription] Transaction failed", zap.Any("error", err.Error()))
	}
	// Check subscription expire
	err = l.svc.Store.InTx(ctx, func(store repository.Store) error {
		list, err := store.User().FindExpiredSubscribes(ctx, time.Now())
		if err != nil {
			zap.S().Error("[Check Subscription] Find subscribe failed", zap.Any("error", err.Error()))
			return err
		}
		var ids []int64
		for _, item := range list {
			ids = append(ids, item.Id)
		}
		if len(ids) > 0 {
			if err = store.User().MarkSubscribesFinished(ctx, ids, 3, time.Now()); err != nil {
				zap.S().Error("[Check Subscription Expire] Update subscribe status failed", zap.Any("error", err.Error()))
				return err
			}
			err = l.sendExpiredNotify(ctx, ids)
			if err != nil {
				zap.S().Error("[Check Subscription Expire] Send email failed", zap.Any("error", err.Error()))
				return nil
			}
			l.clearServerCache(ctx, list...)

			zap.S().Info("[Check Subscription Expire] Update subscribe status", zap.Any("user_ids", ids), zap.Any("count", int64(len(ids))))
		} else {
			zap.S().Info("[Check Subscription Expire] No subscribe need to update")
		}

		return nil
	})
	if err != nil {
		zap.S().Info("[CheckSubscription] Transaction failed", zap.Any("error", err.Error()))
	}
	return nil
}

func (l *CheckSubscriptionLogic) sendExpiredNotify(ctx context.Context, subs []int64) error {
	for _, id := range subs {
		sub, err := l.svc.Store.User().FindOneUserSubscribe(ctx, id)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] FindOneUserSubscribe failed", zap.Any("error", err.Error()))
			continue
		}
		method, err := l.svc.Store.User().FindUserAuthMethodByUserId(ctx, "email", sub.UserId)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] FindUserAuthMethodByUserId failed", zap.Any("error", err.Error()), zap.Any("user_id", sub.UserId))
			continue
		}
		var taskPayload queue.SendEmailPayload
		taskPayload.Type = queue.EmailTypeExpiration
		taskPayload.Email = method.AuthIdentifier
		taskPayload.Subject = "Subscription Expired"
		taskPayload.Content = map[string]interface{}{
			"SiteLogo":   l.svc.Config.Site.SiteLogo,
			"SiteName":   l.svc.Config.Site.SiteName,
			"ExpireDate": sub.ExpireTime.Format("2006-01-02 15:04:05"),
		}
		payloadBuy, err := json.Marshal(taskPayload)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] Marshal payload failed", zap.Any("error", err.Error()))
			continue
		}
		task := asynq.NewTask(queue.ForthwithSendEmail, payloadBuy, asynq.MaxRetry(3))
		taskInfo, err := l.svc.Queue.Enqueue(task)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] Enqueue task failed", zap.Any("error", err.Error()), zap.Any("payload", string(payloadBuy)))
			continue
		}
		zap.S().Infow("[CheckSubscription] Send email success",
			zap.Any("taskID", taskInfo.ID), zap.Any("User", sub.UserId),
			zap.Any("Email", method.AuthIdentifier),
		)
	}
	return nil
}

func (l *CheckSubscriptionLogic) sendTrafficNotify(ctx context.Context, subs []int64) error {
	for _, id := range subs {
		sub, err := l.svc.Store.User().FindOneUserSubscribe(ctx, id)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] FindOneUserSubscribe failed", zap.Any("error", err.Error()))
			continue
		}
		method, err := l.svc.Store.User().FindUserAuthMethodByUserId(ctx, "email", sub.UserId)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] FindUserAuthMethodByUserId failed", zap.Any("error", err.Error()), zap.Any("user_id", sub.UserId))
			continue
		}
		var taskPayload queue.SendEmailPayload
		taskPayload.Type = queue.EmailTypeTrafficExceed
		taskPayload.Email = method.AuthIdentifier
		taskPayload.Subject = "Subscription Traffic Exceed"
		taskPayload.Content = map[string]interface{}{
			"SiteLogo": l.svc.Config.Site.SiteLogo,
			"SiteName": l.svc.Config.Site.SiteName,
		}
		payloadBuy, err := json.Marshal(taskPayload)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] Marshal payload failed", zap.Any("error", err.Error()))
			continue
		}
		task := asynq.NewTask(queue.ForthwithSendEmail, payloadBuy, asynq.MaxRetry(3))
		taskInfo, err := l.svc.Queue.Enqueue(task)
		if err != nil {
			zap.S().Errorw("[CheckSubscription] Enqueue task failed", zap.Any("error", err.Error()), zap.Any("payload", string(payloadBuy)))
			continue
		}
		zap.S().Infow("[CheckSubscription] Send email success",
			zap.Any("taskID", taskInfo.ID), zap.Any("User", sub.UserId),
			zap.Any("Email", method.AuthIdentifier),
		)
	}
	return nil
}

func (l *CheckSubscriptionLogic) clearServerCache(ctx context.Context, userSubs ...*user.Subscribe) {
	subs := make(map[int64]bool)
	for _, sub := range userSubs {
		if _, ok := subs[sub.SubscribeId]; !ok {
			subs[sub.SubscribeId] = true
		}
	}

	for sub, _ := range subs {
		if err := l.svc.Store.Subscribe().ClearCache(ctx, sub); err != nil {
			zap.S().Errorw("[CheckSubscription] ClearCache failed", zap.Any("error", err.Error()), zap.Any("subscribe_id", sub))
		}
	}
}
