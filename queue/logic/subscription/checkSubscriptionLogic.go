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
	"github.com/perfect-panel/server/pkg/tasklock"
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
	lock, held, err := tasklock.Acquire(ctx, l.svc.Redis, "subscription:sweep:lock", 5*time.Minute)
	if err != nil || !held {
		return err
	}
	defer lock.Release(ctx)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stop := lock.KeepAlive(ctx, 5*time.Minute, cancel)
	defer stop()
	for _, status := range []uint8{2, 3} {
		var finished []*user.Subscribe
		err := l.svc.Store.InTx(ctx, func(store repository.Store) error {
			var selected []*user.Subscribe
			var err error
			if status == 2 {
				selected, err = store.User().FindTrafficExceededSubscribes(ctx)
			} else {
				selected, err = store.User().FindExpiredSubscribes(ctx, time.Now())
			}
			if err != nil {
				return err
			}
			finished, err = store.User().MarkSubscribesFinished(ctx, finishedSubscriptionIDs(selected), status, time.Now())
			return err
		})
		if err != nil {
			return err
		}
		l.clearServerCache(ctx, finished...)
		if status == 2 {
			err = l.sendTrafficNotify(ctx, finishedSubscriptionIDs(finished))
		} else {
			err = l.sendExpiredNotify(ctx, finishedSubscriptionIDs(finished))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func finishedSubscriptionIDs(subscriptions []*user.Subscribe) []int64 {
	ids := make([]int64, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription != nil {
			ids = append(ids, subscription.Id)
		}
	}
	return ids
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
