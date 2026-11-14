package orderaudit

import (
	"context"
	"time"

	"github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/model/order"
	"github.com/pkg/errors"
)

// Sources describing where an order originated.
const (
	SourceUser  = "user"
	SourceGuest = "guest"
	SourceAdmin = "admin"
)

// LogInserter is the minimal log-repository surface needed to persist an audit
// entry. It matches log.Model so the transaction-scoped store can be passed in.
type LogInserter interface {
	Insert(ctx context.Context, data *log.SystemLog) error
}

// InsertCreated stores a safe order summary and request risk metadata.
func InsertCreated(ctx context.Context, logs LogInserter, data *order.Order, source string) error {
	if logs == nil {
		return errors.New("order audit log repository is unavailable")
	}
	if data == nil {
		return errors.New("order audit data is nil")
	}

	now := time.Now()
	content, err := (&log.OrderCreated{
		Metadata:       log.MetadataFromContext(ctx),
		OrderNo:        data.OrderNo,
		OrderType:      data.Type,
		Quantity:       data.Quantity,
		Price:          data.Price,
		Amount:         data.Amount,
		GiftAmount:     data.GiftAmount,
		Discount:       data.Discount,
		CouponDiscount: data.CouponDiscount,
		PaymentID:      data.PaymentId,
		Method:         data.Method,
		FeeAmount:      data.FeeAmount,
		SubscribeID:    data.SubscribeId,
		Source:         source,
		Timestamp:      now.UnixMilli(),
	}).Marshal()
	if err != nil {
		return errors.Wrap(err, "marshal order audit log")
	}

	return logs.Insert(ctx, &log.SystemLog{
		Type:     log.TypeOrderCreated.Uint8(),
		Date:     now.Format(time.DateOnly),
		ObjectID: data.UserId,
		Content:  string(content),
	})
}
