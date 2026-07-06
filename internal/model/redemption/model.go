package redemption

import (
	"github.com/perfect-panel/server/ent"
)

// NewRedemptionCodeModel returns a model for the redemption_code table.
func NewRedemptionCodeModel(conn *ent.Client) RedemptionCodeModel {
	return &customRedemptionCodeModel{
		defaultRedemptionCodeModel: newRedemptionCodeModel(conn),
	}
}

// NewRedemptionRecordModel returns a model for the redemption_record table.
func NewRedemptionRecordModel(conn *ent.Client) RedemptionRecordModel {
	return &customRedemptionRecordModel{
		defaultRedemptionRecordModel: newRedemptionRecordModel(conn),
	}
}
