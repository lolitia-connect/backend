package redemption

import (
	"context"
	"crypto/rand"
	"github.com/perfect-panel/server/ent"
	"math/big"

	"github.com/perfect-panel/server/internal/model/redemption"
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
func (l *CreateRedemptionCodeLogic) generateUniqueCode() (string, error) {
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
		_, err := l.svcCtx.Store.RedemptionCode().FindOneByCode(l.ctx, codeStr)
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
		l.Logger.Errorw("[CreateRedemptionCode] Subscribe plan cannot be empty")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "subscribe plan cannot be empty")
	}

	// Verify subscribe plan exists
	_, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, req.SubscribePlan)
	if err != nil {
		if ent.IsNotFound(err) {
			l.Logger.Errorw("[CreateRedemptionCode] Subscribe plan not found", zap.Any("subscribe_plan", req.SubscribePlan))
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "subscribe plan not found")
		}
		l.Logger.Errorw("[CreateRedemptionCode] Database Error", zap.Any("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find subscribe plan error: %v", err.Error())
	}

	// Validate batch count
	if req.BatchCount < 1 {
		l.Logger.Errorw("[CreateRedemptionCode] Batch count must be at least 1")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "batch count must be at least 1")
	}

	// Generate redemption codes in batch
	var createdCodes []string
	for i := int64(0); i < req.BatchCount; i++ {
		code, err := l.generateUniqueCode()
		if err != nil {
			l.Logger.Errorw("[CreateRedemptionCode] Failed to generate unique code", zap.Any("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "generate unique code error: %v", err.Error())
		}

		redemptionCode := &redemption.RedemptionCode{
			Code:          code,
			TotalCount:    req.TotalCount,
			UsedCount:     0,
			SubscribePlan: req.SubscribePlan,
			UnitTime:      normalizeUnitTime(req.UnitTime),
			Quantity:      req.Quantity,
			Status:        1, // Default to enabled
		}

		err = l.svcCtx.Store.RedemptionCode().Insert(l.ctx, redemptionCode)
		if err != nil {
			l.Logger.Errorw("[CreateRedemptionCode] Database Error", zap.Any("error", err.Error()))
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create redemption code error: %v", err.Error())
		}

		createdCodes = append(createdCodes, code)
	}

	l.Logger.Infow("[CreateRedemptionCode] Successfully created redemption codes",
		zap.Any("count", len(createdCodes)),
		zap.Any("codes", createdCodes))

	return nil
}
