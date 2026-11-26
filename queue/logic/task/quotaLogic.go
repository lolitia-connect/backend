package task

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/perfect-panel/server/internal/model/wallet"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/ent"
	enttask "github.com/perfect-panel/server/ent/task"
	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/task"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

const (
	UnitTimeNoLimit = "NoLimit" // Unlimited time subscription
	UnitTimeYear    = "Year"    // Annual subscription
	UnitTimeMonth   = "Month"   // Monthly subscription
	UnitTimeDay     = "Day"     // Daily subscription
	UnitTimeHour    = "Hour"    // Hourly subscription
	UnitTimeMinute  = "Minute"  // Per-minute subscription

)

type QuotaTaskLogic struct {
	svcCtx *svc.ServiceContext
}

type ErrorInfo struct {
	UserSubscribeId int64  `json:"user_subscribe_id"`
	Error           string `json:"error"`
}

func NewQuotaTaskLogic(svcCtx *svc.ServiceContext) *QuotaTaskLogic {
	return &QuotaTaskLogic{
		svcCtx: svcCtx,
	}
}

func (l *QuotaTaskLogic) ProcessTask(ctx context.Context, t *asynq.Task) error {
	taskID, err := l.parseTaskID(ctx, t.Payload())
	if err != nil {
		return err
	}

	taskInfo, err := l.getTaskInfo(ctx, taskID)
	if err != nil {
		return err
	}

	if taskInfo.Status != 0 {
		zap.S().Info("[QuotaTaskLogic.ProcessTask] task already processed",
			zap.Any("taskID", taskID),
			zap.Any("status", taskInfo.Status),
		)
		return nil
	}

	scope, content, err := l.parseTaskData(ctx, taskInfo)
	if err != nil {
		return err
	}

	subscribes, err := l.getSubscribes(ctx, scope.Objects)
	if err != nil {
		return err
	}
	if err = l.processSubscribes(ctx, subscribes, content, taskInfo); err != nil {
		return err
	}
	return nil
}

func (l *QuotaTaskLogic) parseTaskID(ctx context.Context, payload []byte) (int64, error) {
	if len(payload) == 0 {
		zap.S().Error("[QuotaTaskLogic.parseTaskID] empty payload")
		return 0, asynq.SkipRetry
	}

	taskID, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		zap.S().Error("[QuotaTaskLogic.parseTaskID] invalid task ID",
			zap.Any("error", err.Error()),
			zap.Any("payload", string(payload)),
		)
		return 0, asynq.SkipRetry
	}
	return taskID, nil
}

func (l *QuotaTaskLogic) getTaskInfo(ctx context.Context, taskID int64) (*task.Task, error) {
	taskInfo, err := l.svcCtx.Store.Task().FindOne(ctx, taskID)
	if err != nil {
		zap.S().Error("[QuotaTaskLogic.getTaskInfo] find task error",
			zap.Any("error", err.Error()),
			zap.Any("taskID", taskID),
		)
		if ent.IsNotFound(err) {
			return nil, asynq.SkipRetry
		}
		return nil, err
	}
	return taskInfo, nil
}

func (l *QuotaTaskLogic) parseTaskData(ctx context.Context, taskInfo *task.Task) (task.QuotaScope, task.QuotaContent, error) {
	var scope task.QuotaScope
	if err := scope.Unmarshal([]byte(taskInfo.Scope)); err != nil {
		zap.S().Error("[QuotaTaskLogic.parseTaskData] unmarshal scope error",
			zap.Any("error", err.Error()),
		)
		return scope, task.QuotaContent{}, asynq.SkipRetry
	}

	var content task.QuotaContent
	if err := content.Unmarshal([]byte(taskInfo.Content)); err != nil {
		zap.S().Error("[QuotaTaskLogic.parseTaskData] unmarshal content error",
			zap.Any("error", err.Error()),
		)
		return scope, content, asynq.SkipRetry
	}
	return scope, content, nil
}

func (l *QuotaTaskLogic) getSubscribes(ctx context.Context, subscriberIDs []int64) ([]*user.Subscribe, error) {
	subscribes, err := l.svcCtx.Store.User().FindSubscribesByIds(ctx, subscriberIDs)
	if err != nil {
		zap.S().Error("[QuotaTaskLogic.getSubscribes] find subscribes error",
			zap.Any("error", err.Error()),
			zap.Any("subscribers", subscriberIDs),
		)
		return nil, err
	}
	return subscribes, nil
}

func (l *QuotaTaskLogic) processSubscribes(ctx context.Context, subscribes []*user.Subscribe, content task.QuotaContent, taskInfo *task.Task) error {
	return l.svcCtx.Store.InTx(ctx, func(store repository.Store) error {
		// Claim within the grant transaction so duplicate deliveries cannot apply it twice.
		claimed, err := store.Ent().Task.Update().Where(enttask.ID(taskInfo.Id), enttask.TypeEQ(int8(task.TypeQuota)), enttask.StatusEQ(0)).SetStatus(1).Save(ctx)
		if err != nil || claimed == 0 {
			return err
		}
		var errors []ErrorInfo
		now := time.Now()

		wallets := make(map[int64]*wallet.Wallet)
		if content.GiftValue > 0 && (content.GiftType == 1 || content.GiftType == 2) {
			for _, sub := range subscribes {
				if sub != nil && sub.EntitlementSource == "" {
					wallets[sub.UserId] = nil
				}
			}
			// Billing flows lock wallets before subscriptions. Sort owners to avoid reversing that order between grants.
			for _, id := range slices.Sorted(maps.Keys(wallets)) {
				w, err := store.Wallet().FindOneForUpdate(ctx, id)
				if ent.IsNotFound(err) {
					continue
				}
				if err != nil {
					return err
				}
				wallets[id] = w
			}
		}
		for _, sub := range subscribes {
			if err := l.processSubscription(ctx, store, sub, wallets, content, now, &errors); err != nil {
				return err
			}
		}

		// 根据错误情况决定任务状态
		status := int8(2) // Completed
		if len(errors) > 0 {
			zap.S().Error("[QuotaTaskLogic.processSubscribes] some subscriptions failed",
				zap.Any("total", len(subscribes)),
				zap.Any("failed", len(errors)),
			)
			// 如果所有订阅都失败，标记为失败状态
			if len(errors) == len(subscribes) {
				status = 3 // Failed
			}
			errs, err := json.Marshal(errors)
			if err != nil {
				zap.S().Error("[QuotaTaskLogic.processSubscribes] marshal errors failed",
					zap.Any("error", err.Error()),
				)
				return err
			}
			taskInfo.Errors = string(errs)
		}

		if err := store.Ent().Task.UpdateOneID(taskInfo.Id).SetCurrent(uint64(len(subscribes))).SetStatus(status).SetErrors(taskInfo.Errors).Exec(ctx); err != nil {
			zap.S().Error("[QuotaTaskLogic.processSubscribes] update task status error",
				zap.Any("error", err.Error()),
				zap.Any("taskID", taskInfo.Id),
			)
			return err
		}

		return nil
	})
}

// processSubscription applies one subscription's share of a quota task.
//
// The row is re-read under a write lock inside the caller's transaction and only
// the columns the grant actually changes are written (see ApplyQuotaGrant). The
// snapshot collected before the transaction must not be saved back: it predates
// any traffic settlement, credential rotation or admin edit that happened in
// between, so a whole-row write would silently revert them.
func (l *QuotaTaskLogic) processSubscription(ctx context.Context, store repository.Store, sub *user.Subscribe, wallets map[int64]*wallet.Wallet, content task.QuotaContent, now time.Time, errors *[]ErrorInfo) error {
	// 验证订阅数据
	if sub == nil {
		*errors = append(*errors, ErrorInfo{
			UserSubscribeId: 0,
			Error:           "subscription is nil",
		})
		return nil
	}
	if sub.EntitlementSource != "" {
		return nil
	}

	current, err := store.User().FindOneSubscribeForUpdate(ctx, sub.Id)
	if err != nil {
		if ent.IsNotFound(err) {
			// The subscription was removed between the snapshot and the
			// transaction; skip it rather than failing the whole task.
			*errors = append(*errors, ErrorInfo{
				UserSubscribeId: sub.Id,
				Error:           "subscription no longer exists",
			})
			return nil
		}
		return err
	}
	if current == nil {
		return nil
	}
	if current.EntitlementSource != "" {
		return nil
	}
	// Keep the caller's snapshot in step for the log writers below, which read
	// the identity fields only.
	sub.UserId, sub.SubscribeId = current.UserId, current.SubscribeId

	grant := user.QuotaGrant{}

	// 处理时间延长 - 修复逻辑：只要Days不为0就处理，不管ExpireTime是否为0
	if content.Days != 0 {
		expireTime := current.ExpireTime
		if expireTime.Unix() == 0 || expireTime.Before(now) {
			// 如果没有过期时间或已过期，从现在开始计算
			expireTime = now.AddDate(0, 0, int(content.Days))
		} else {
			// 在原有过期时间基础上延长
			expireTime = expireTime.AddDate(0, 0, int(content.Days))
		}
		grant.ExpireTime = &expireTime
		// 如果订阅延长到未来时间，设置为激活状态
		if expireTime.After(now) && current.Status != 1 {
			active := uint8(1)
			grant.Status = &active
		}
	}

	// 处理流量重置
	//
	// The grant is applied first and the audit log is written only once the
	// counters were actually zeroed. ApplyQuotaGrant refuses a row the CAS
	// predicate rejects (status >= 4, provider-managed) and reports that as an
	// error rather than rolling the transaction back, so writing the log first
	// would leave a durable "traffic reset" entry for a subscription that was
	// never reset.
	grant.ResetTraffic = content.ResetTraffic

	if err := store.User().ApplyQuotaGrant(ctx, current.Id, grant); err != nil {
		*errors = append(*errors, ErrorInfo{
			UserSubscribeId: current.Id,
			Error:           "update subscription error: " + err.Error(),
		})
		return nil
	}

	if content.ResetTraffic {
		if err := l.createResetTrafficLog(ctx, store, current.Id, current.UserId, now); err != nil {
			// 记录错误但不阻断整个任务,日志失败不影响主流程
			*errors = append(*errors, ErrorInfo{
				UserSubscribeId: current.Id,
				Error:           "create reset traffic log error: " + err.Error(),
			})
		}
	}

	// 处理赠送金
	if content.GiftValue != 0 {
		if err := l.processGift(ctx, store, current, wallets[current.UserId], content, now, errors); err != nil {
			return err
		}
	}

	return nil
}

func (l *QuotaTaskLogic) processGift(ctx context.Context, store repository.Store, sub *user.Subscribe, walletInfo *wallet.Wallet, content task.QuotaContent, now time.Time, errors *[]ErrorInfo) error {
	// 验证赠送类型
	if content.GiftType != 1 && content.GiftType != 2 {
		*errors = append(*errors, ErrorInfo{
			UserSubscribeId: sub.Id,
			Error:           fmt.Sprintf("invalid gift type: %d", content.GiftType),
		})
		return nil
	}

	if walletInfo == nil {
		*errors = append(*errors, ErrorInfo{UserSubscribeId: sub.Id, Error: "gift recipient no longer exists"})
		return nil
	}
	var giftAmount int64
	switch content.GiftType {
	case 1:
		giftAmount = int64(content.GiftValue)
	case 2:
		// 获取订阅对应的套餐信息
		subscribeInfo, err := store.Subscribe().FindOne(ctx, sub.SubscribeId)
		if err != nil {
			*errors = append(*errors, ErrorInfo{
				UserSubscribeId: sub.Id,
				Error:           "find subscribe error: " + err.Error(),
			})
			return nil
		}
		if subscribeInfo.UnitPrice > 0 {
			giftAmount = int64(float64(subscribeInfo.UnitPrice) * (float64(content.GiftValue) / 100))
		}
	}

	if giftAmount > 0 {
		walletInfo.GiftAmount += giftAmount
		if err := store.Wallet().UpdateBalanceFields(ctx, walletInfo); err != nil {
			return err
		}
		return l.createGiftLog(ctx, store, sub.Id, sub.UserId, giftAmount, walletInfo.GiftAmount, now)
	}

	return nil
}

func (l *QuotaTaskLogic) getStartTime(sub *user.Subscribe, now time.Time) time.Time {
	if sub.StartTime.Unix() == 0 {
		return now
	}
	return sub.StartTime
}

func (l *QuotaTaskLogic) createGiftLog(ctx context.Context, store repository.Store, subscribeId, userId, amount, balance int64, now time.Time) error {
	giftLog := &log.Gift{
		Type:        log.GiftTypeIncrease,
		OrderNo:     "",
		SubscribeId: subscribeId,
		Amount:      amount,
		Balance:     balance,
		Remark:      "Quota task gift",
		Timestamp:   now.UnixMilli(),
	}

	logString, err := giftLog.Marshal()
	if err != nil {
		return fmt.Errorf("marshal gift log error: %v", err)
	}
	return store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeGift.Uint8(),
		Content:  string(logString),
		ObjectID: userId,
		Date:     now.Format(time.DateOnly),
	})
}

func (l *QuotaTaskLogic) createResetTrafficLog(ctx context.Context, store repository.Store, subscribeId, userId int64, now time.Time) error {
	trafficLog := &log.ResetSubscribe{
		Type:      log.ResetSubscribeTypeQuota,
		UserId:    userId,
		OrderNo:   "",
		Timestamp: now.UnixMilli(),
	}

	logString, err := trafficLog.Marshal()
	if err != nil {
		return fmt.Errorf("marshal traffic log error: %v", err)
	}
	return store.Log().Insert(ctx, &log.SystemLog{
		Type:     log.TypeResetSubscribe.Uint8(),
		Content:  string(logString),
		ObjectID: subscribeId,
		Date:     now.Format(time.DateOnly),
	})
}
