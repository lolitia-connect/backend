package redemption

import (
	"context"
	"crypto/rand"
	"math/big"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/redemption"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type CreateRedemptionCodeLogic struct {
	Logger *zap.SugaredLogger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Create redemption code
func NewCreateRedemptionCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateRedemptionCodeLogic {
	return &CreateRedemptionCodeLogic{
		Logger: zap.S(),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// generateUniqueCode generates a unique redemption code
func (l *CreateRedemptionCodeLogic) generateUniqueCode(codes redemption.RedemptionCodeModel) (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Removed confusing characters like I, O, 0, 1
	const codeLength = 16

	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		code := make([]byte, codeLength)
		for j := 0; j < codeLength; j++ {
			num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
			if err != nil {
				return "", err
			}
			code[j] = charset[num.Int64()]
		}

		codeStr := string(code)

		// Check if code already exists
		_, err := codes.FindOneByCode(l.ctx, codeStr)
		if ent.IsNotFound(err) {
			return codeStr, nil
		} else if err != nil {
			return "", err
		}
		// Code exists, try again
	}

	return "", errors.New("failed to generate unique code after maximum retries")
}

func (l *CreateRedemptionCodeLogic) CreateRedemptionCode(req *types.CreateRedemptionCodeRequest) error {
	// Check if subscribe plan is valid
	if req.SubscribePlan == 0 {
		l.Logger.Debugw("[CreateRedemptionCode] Subscribe plan cannot be empty")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "subscribe plan cannot be empty")
	}

	// Verify subscribe plan exists
	_, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.SubscribePlan)
	if err != nil {
		if ent.IsNotFound(err) {
			l.Logger.Debugw("[CreateRedemptionCode] Subscribe plan not found", zap.Any("subscribe_plan", req.SubscribePlan))
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "subscribe plan not found")
		}
		l.Logger.Errorw("[CreateRedemptionCode] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe plan error: %v", err.Error())
	}

	// Validate batch count
	if req.BatchCount < 1 {
		l.Logger.Debug("[CreateRedemptionCode] Batch count must be at least 1")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "batch count must be at least 1")
	}

	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		codes := store.RedemptionCode()
		for i := int64(0); i < req.BatchCount; i++ {
			code, err := l.generateUniqueCode(codes)
			if err != nil {
				return errors.Wrap(err, "generate unique code")
			}
			if err := codes.Insert(l.ctx, &redemption.RedemptionCode{
				Code:          code,
				TotalCount:    req.TotalCount,
				SubscribePlan: req.SubscribePlan,
				UnitTime:      normalizeUnitTime(req.UnitTime),
				Quantity:      req.Quantity,
				Status:        1,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		l.Logger.Errorw("[CreateRedemptionCode] Batch creation failed", zap.Error(err))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create redemption codes: %v", err)
	}
	l.Logger.Infow("[CreateRedemptionCode] Successfully created redemption codes", "count", req.BatchCount)

	return nil
}
