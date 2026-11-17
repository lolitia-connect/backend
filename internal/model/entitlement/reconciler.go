package entitlement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/perfect-panel/server/ent"
	entorder "github.com/perfect-panel/server/ent/order"
	entsubscribe "github.com/perfect-panel/server/ent/subscribe"
	entsubentitlement "github.com/perfect-panel/server/ent/subscriptionentitlement"
	entsubscriptionperiod "github.com/perfect-panel/server/ent/subscriptionperiod"
	entuser "github.com/perfect-panel/server/ent/user"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/uuidx"
)

const AppleIAPOrderMethod = "AppleIAP"

var ErrEntitlementConflict = errors.New("entitlement ownership or revision conflict")

type EntitlementResult struct {
	UserSubscribeID int64
	Revision        int64
	Applied         bool
	AccessUntil     time.Time
}

type Reconciler struct {
	store       repository.Store
	singleModel func() bool
}

func NewReconciler(store repository.Store, singleModel ...func() bool) *Reconciler {
	reconciler := &Reconciler{store: store}
	if len(singleModel) > 0 {
		reconciler.singleModel = singleModel[0]
	}
	return reconciler
}

// Reconcile accepts a provider-authenticated, already verified entitlement
// snapshot. Apple signature verification and transaction history resolution
// belong to the billing adapter that calls this method.
func (r *Reconciler) Reconcile(ctx context.Context, command ReconcileEntitlementCommand) (*EntitlementResult, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	normalized, err := command.Normalize(now)
	if err != nil {
		return nil, err
	}
	normalized.PeriodStart = normalized.PeriodStart.UTC().Truncate(time.Millisecond)
	normalized.PeriodEnd = normalized.PeriodEnd.UTC().Truncate(time.Millisecond)
	normalized.GraceUntil = canonicalTime(normalized.GraceUntil)
	normalized.RevokedAt = canonicalTime(normalized.RevokedAt)
	if err := normalized.Validate(now); err != nil {
		return nil, err
	}
	providerOrder, err := r.store.Order().FindOne(ctx, normalized.OrderID)
	if err != nil {
		return nil, err
	}
	if !validAppleOrder(providerOrder, normalized) {
		return nil, ErrEntitlementConflict
	}

	stateID := entitlementKey(normalized.Source, normalized.Scope, normalized.SubscriptionKey)
	periodID := entitlementKey(normalized.Source, normalized.Scope, normalized.TransactionKey)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	fingerprint := entitlementKey(string(payload))
	result := &EntitlementResult{}
	var projectedSubscription *ent.UserSubscribe
	var cachePlanIDs []int64

	err = r.store.InTx(ctx, func(txStore repository.Store) error {
		if err := lockUser(ctx, txStore.Ent(), normalized.UserID); err != nil {
			return err
		}
		orderQuery := txStore.Ent().Order.Query().Where(entorder.ID(normalized.OrderID))
		if txStore.Ent().Driver().Dialect() != dialect.SQLite {
			orderQuery = orderQuery.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
		}
		providerOrder, err := orderQuery.Only(ctx)
		if err != nil {
			return err
		}
		if providerOrder.UserID != normalized.UserID || providerOrder.SubscribeID != normalized.PlanID ||
			providerOrder.TradeNo == nil || *providerOrder.TradeNo != normalized.TransactionKey || providerOrder.Method != AppleIAPOrderMethod ||
			(providerOrder.Status != order.StatusPaid && providerOrder.Status != order.StatusFinished) {
			return ErrEntitlementConflict
		}

		state, err := findStateForUpdate(ctx, txStore.Ent(), stateID)
		if err != nil {
			return err
		}
		if state != nil {
			if state.UserID != normalized.UserID || state.Source != normalized.Source || state.Mode != normalized.Mode {
				return ErrEntitlementConflict
			}
			cachePlanIDs, err = entitlementPlanIDs(ctx, txStore.Ent(), stateID, normalized.PlanID)
			if err != nil {
				return err
			}
			projectedSubscription, err = findSubscriptionForUpdate(ctx, txStore.Ent(), state.UserSubscribeID)
			if err != nil {
				return err
			}
			if projectedSubscription.UserID != normalized.UserID || projectedSubscription.EntitlementSource != normalized.Source {
				return ErrEntitlementConflict
			}
			if normalized.Revision < state.Revision {
				result.UserSubscribeID = projectedSubscription.ID
				result.Revision = state.Revision
				result.AccessUntil = projectedSubscription.ExpireTime
				return nil
			}
			if normalized.Revision == state.Revision {
				if fingerprint != state.Fingerprint {
					return ErrEntitlementConflict
				}
				result.UserSubscribeID = projectedSubscription.ID
				result.Revision = state.Revision
				result.AccessUntil = projectedSubscription.ExpireTime
				return nil
			}
		}

		plan, err := txStore.Ent().Subscribe.Query().Where(entsubscribe.ID(normalized.PlanID)).Only(ctx)
		if err != nil {
			return err
		}
		period, err := findPeriod(ctx, txStore.Ent(), periodID)
		if err != nil {
			return err
		}
		if period != nil {
			expectedSubscriptionID := int64(0)
			if state != nil {
				expectedSubscriptionID = state.UserSubscribeID
			}
			if period.EntitlementID != stateID || period.TransactionKey != normalized.TransactionKey ||
				period.UserSubscribeID != expectedSubscriptionID || period.OrderID != normalized.OrderID ||
				period.PlanID != normalized.PlanID || period.BillingInterval != normalized.BillingInterval ||
				!period.StartAt.Equal(normalized.PeriodStart) {
				return ErrEntitlementConflict
			}
		}

		now = time.Now().UTC().Truncate(time.Millisecond)
		accessUntil := accessUntil(normalized)
		trafficLimit := plan.Traffic
		trafficUnlimited := plan.TrafficUnlimited
		if period != nil {
			trafficLimit = period.TrafficLimit
			trafficUnlimited = projectedSubscription.TrafficUnlimited
		}
		trafficReset := shouldResetTraffic(normalized, plan.ResetCycle, period == nil, now)

		if state == nil {
			if r.singleModel != nil && r.singleModel() {
				blocked, err := txStore.Ent().UserSubscribe.Query().
					Where(entsub.UserID(normalized.UserID), entsub.StatusNEQ(4)).
					Exist(ctx)
				if err != nil {
					return err
				}
				if blocked {
					return fmt.Errorf("single subscription mode exceeds limit")
				}
			}
			if err := ensureSubscriptionQuota(ctx, txStore.Ent(), normalized.UserID, normalized.PlanID, plan.Quota); err != nil {
				return err
			}
			projectedSubscription, err = createProviderSubscription(ctx, txStore.Ent(), normalized, plan, accessUntil, now)
			if err != nil {
				return err
			}
		} else {
			if err := applyProjection(ctx, txStore.Ent(), projectedSubscription, normalized, accessUntil, trafficLimit, trafficUnlimited, trafficReset, now); err != nil {
				return err
			}
		}
		if state == nil {
			cachePlanIDs, err = entitlementPlanIDs(ctx, txStore.Ent(), stateID, normalized.PlanID)
			if err != nil {
				return err
			}
		}

		if period == nil {
			_, err = txStore.Ent().SubscriptionPeriod.Create().
				SetID(periodID).
				SetEntitlementID(stateID).
				SetTransactionKey(normalized.TransactionKey).
				SetUserSubscribeID(projectedSubscription.ID).
				SetOrderID(normalized.OrderID).
				SetPlanID(normalized.PlanID).
				SetBillingInterval(normalized.BillingInterval).
				SetTrafficLimit(trafficLimit).
				SetStartAt(normalized.PeriodStart).
				SetEndAt(normalized.PeriodEnd).
				SetTrafficReset(trafficReset).
				Save(ctx)
			if err != nil {
				return err
			}
		} else if err := txStore.Ent().SubscriptionPeriod.UpdateOneID(period.ID).
			SetEndAt(normalized.PeriodEnd).
			SetUpdatedAt(now).
			Exec(ctx); err != nil {
			return err
		}

		if state == nil {
			_, err = txStore.Ent().SubscriptionEntitlement.Create().
				SetID(stateID).
				SetSource(normalized.Source).
				SetScope(normalized.Scope).
				SetSubscriptionKey(normalized.SubscriptionKey).
				SetUserID(normalized.UserID).
				SetUserSubscribeID(projectedSubscription.ID).
				SetRevision(normalized.Revision).
				SetFingerprint(fingerprint).
				SetPeriodID(periodID).
				SetMode(normalized.Mode).
				SetStatus(normalized.Status).
				SetAutoRenew(normalized.AutoRenew).
				SetNillableGraceUntil(normalized.GraceUntil).
				SetNillableRevokedAt(normalized.RevokedAt).
				Save(ctx)
			if err != nil {
				return err
			}
		} else {
			update := txStore.Ent().SubscriptionEntitlement.UpdateOneID(stateID).
				SetRevision(normalized.Revision).
				SetFingerprint(fingerprint).
				SetPeriodID(periodID).
				SetStatus(normalized.Status).
				SetAutoRenew(normalized.AutoRenew).
				SetUpdatedAt(now)
			if normalized.GraceUntil == nil {
				update.ClearGraceUntil()
			} else {
				update.SetGraceUntil(*normalized.GraceUntil)
			}
			if normalized.RevokedAt == nil {
				update.ClearRevokedAt()
			} else {
				update.SetRevokedAt(*normalized.RevokedAt)
			}
			if _, err := update.Save(ctx); err != nil {
				return err
			}
		}

		if err := txStore.Ent().SubscriptionEntitlementRevision.Create().
			SetID(stateID + ":" + strconv.FormatInt(normalized.Revision, 10)).
			SetEntitlementID(stateID).
			SetPayload(string(payload)).
			Exec(ctx); err != nil {
			return err
		}

		result.UserSubscribeID = projectedSubscription.ID
		result.Revision = normalized.Revision
		result.Applied = true
		result.AccessUntil = accessUntil
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := clearPlanCaches(ctx, r.store, cachePlanIDs); err != nil {
		return nil, err
	}
	return result, nil
}

func validAppleOrder(providerOrder *order.Order, command ReconcileEntitlementCommand) bool {
	return providerOrder != nil && providerOrder.UserId == command.UserID && providerOrder.SubscribeId == command.PlanID &&
		providerOrder.TradeNo == command.TransactionKey && providerOrder.Method == AppleIAPOrderMethod &&
		(providerOrder.Status == order.StatusPaid || providerOrder.Status == order.StatusFinished)
}

func entitlementKey(parts ...string) string {
	payload, _ := json.Marshal(parts)
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func LocalPeriodID(orderNo string) string {
	return entitlementKey("local", orderNo)
}

func LocalEntitlementID(userSubscribeID int64) string {
	return entitlementKey("local", strconv.FormatInt(userSubscribeID, 10))
}

func uniquePlanIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func entitlementPlanIDs(ctx context.Context, db *ent.Client, entitlementID string, currentPlanID int64) ([]int64, error) {
	planIDs := []int64{currentPlanID}
	previousPlanIDs, err := db.SubscriptionPeriod.Query().
		Where(entsubscriptionperiod.EntitlementID(entitlementID)).
		Select(entsubscriptionperiod.FieldPlanID).
		Ints(ctx)
	if err != nil {
		return nil, err
	}
	for _, planID := range previousPlanIDs {
		planIDs = append(planIDs, int64(planID))
	}
	return uniquePlanIDs(planIDs), nil
}

func clearPlanCaches(ctx context.Context, store repository.Store, planIDs []int64) error {
	planIDs = uniquePlanIDs(planIDs)
	if len(planIDs) == 0 {
		return nil
	}
	existingPlanIDs, err := store.Ent().Subscribe.Query().Where(entsubscribe.IDIn(planIDs...)).IDs(ctx)
	if err != nil {
		return err
	}
	return store.Subscribe().ClearCache(ctx, existingPlanIDs...)
}

func canonicalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := value.UTC().Truncate(time.Millisecond)
	return &canonical
}

func accessUntil(command ReconcileEntitlementCommand) time.Time {
	until := command.PeriodEnd
	if command.Status == StatusGrace && command.GraceUntil != nil {
		until = *command.GraceUntil
	}
	if command.RevokedAt != nil && command.RevokedAt.Before(until) {
		until = *command.RevokedAt
	}
	return until
}

func shouldResetTraffic(command ReconcileEntitlementCommand, resetCycle int64, newPeriod bool, now time.Time) bool {
	return newPeriod && command.ResetTraffic && resetCycle == 0 && command.Status == StatusActive &&
		!command.PeriodStart.After(now) && command.PeriodEnd.After(now)
}

func lockUser(ctx context.Context, db *ent.Client, userID int64) error {
	query := db.User.Query().Where(entuser.ID(userID))
	if db.Driver().Dialect() != dialect.SQLite {
		query = query.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	_, err := query.Only(ctx)
	return err
}

func findStateForUpdate(ctx context.Context, db *ent.Client, id string) (*ent.SubscriptionEntitlement, error) {
	query := db.SubscriptionEntitlement.Query().Where(entsubentitlement.ID(id))
	if db.Driver().Dialect() != dialect.SQLite {
		query = query.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	state, err := query.Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return state, err
}

func findPeriod(ctx context.Context, db *ent.Client, id string) (*ent.SubscriptionPeriod, error) {
	period, err := db.SubscriptionPeriod.Query().Where(entsubscriptionperiod.ID(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return period, err
}

func findSubscriptionForUpdate(ctx context.Context, db *ent.Client, id int64) (*ent.UserSubscribe, error) {
	query := db.UserSubscribe.Query().Where(entsub.ID(id))
	if db.Driver().Dialect() != dialect.SQLite {
		query = query.Where(func(selector *entsql.Selector) { selector.ForUpdate() })
	}
	return query.Only(ctx)
}

func ensureSubscriptionQuota(ctx context.Context, db *ent.Client, userID, planID, quota int64) error {
	if quota <= 0 {
		return nil
	}
	count, err := db.UserSubscribe.Query().Where(entsub.UserID(userID), entsub.SubscribeID(planID), entsub.StatusNEQ(4)).Count(ctx)
	if err != nil {
		return err
	}
	if int64(count) >= quota {
		return fmt.Errorf("subscribe quota limit exceeded")
	}
	return nil
}

func createProviderSubscription(ctx context.Context, db *ent.Client, command ReconcileEntitlementCommand, plan *ent.Subscribe, until, now time.Time) (*ent.UserSubscribe, error) {
	status, finishedAt := projectedStatus(command, until, now, plan.TrafficUnlimited, plan.Traffic, 0, 0)
	return db.UserSubscribe.Create().
		SetUserID(command.UserID).
		SetOrderID(command.OrderID).
		SetSubscribeID(command.PlanID).
		SetEntitlementSource(command.Source).
		SetNodeGroupID(plan.NodeGroupID).
		SetGroupLocked(false).
		SetStartTime(command.PeriodStart).
		SetExpireTime(until).
		SetNillableFinishedAt(finishedAt).
		SetTraffic(plan.Traffic).
		SetTrafficUnlimited(plan.TrafficUnlimited).
		SetDownload(0).
		SetUpload(0).
		SetExpiredDownload(0).
		SetExpiredUpload(0).
		SetToken(uuidx.SubscribeToken("entitlement:" + command.Scope + ":" + command.SubscriptionKey)).
		SetUUID(uuid.NewString()).
		SetStatus(status).
		SetNote("").
		Save(ctx)
}

func applyProjection(ctx context.Context, db *ent.Client, subscription *ent.UserSubscribe, command ReconcileEntitlementCommand, until time.Time, trafficLimit int64, trafficUnlimited bool, resetTraffic bool, now time.Time) error {
	update := db.UserSubscribe.UpdateOneID(subscription.ID).
		Where(entsub.EntitlementSource(command.Source)).
		SetSubscribeID(command.PlanID).
		SetExpireTime(until).
		SetTraffic(trafficLimit).
		SetTrafficUnlimited(trafficUnlimited)
	upload, download := subscription.Upload, subscription.Download
	if resetTraffic {
		upload, download = 0, 0
		update.SetDownload(0).SetUpload(0).SetExpiredDownload(0).SetExpiredUpload(0)
	}
	if subscription.Status != 4 && subscription.Status != 5 {
		status, finishedAt := projectedStatus(command, until, now, trafficUnlimited, trafficLimit, upload, download)
		update.SetStatus(status)
		if finishedAt == nil {
			update.ClearFinishedAt()
		} else {
			update.SetFinishedAt(*finishedAt)
		}
	}
	if _, err := update.Save(ctx); err != nil {
		return err
	}
	return nil
}

func projectedStatus(command ReconcileEntitlementCommand, until, now time.Time, trafficUnlimited bool, trafficLimit, upload, download int64) (uint8, *time.Time) {
	if command.Status == StatusRevoked || command.Status == StatusExpired || command.Status == StatusBillingRetry || !until.After(now) || command.PeriodStart.After(now) {
		return 3, timePtr(now)
	}
	if !trafficUnlimited && trafficLimit > 0 && upload+download >= trafficLimit {
		return 2, timePtr(now)
	}
	return 1, nil
}

func timePtr(value time.Time) *time.Time {
	return &value
}
