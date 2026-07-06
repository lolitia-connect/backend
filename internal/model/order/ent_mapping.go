package order

import (
	"github.com/perfect-panel/server/ent"
	paymentmodel "github.com/perfect-panel/server/internal/model/payment"
	subscribemodel "github.com/perfect-panel/server/internal/model/subscribe"
)

const balancePaymentID int64 = -1

func entToOrder(data *ent.Order) *Order {
	if data == nil {
		return nil
	}
	return &Order{
		Id:             data.ID,
		ParentId:       data.ParentID,
		UserId:         data.UserID,
		OrderNo:        data.OrderNo,
		Type:           data.Type,
		Quantity:       data.Quantity,
		Price:          data.Price,
		Amount:         data.Amount,
		GiftAmount:     data.GiftAmount,
		Discount:       data.Discount,
		Coupon:         stringValue(data.Coupon),
		CouponDiscount: data.CouponDiscount,
		Commission:     data.Commission,
		PaymentId:      data.PaymentID,
		Method:         data.Method,
		FeeAmount:      data.FeeAmount,
		TradeNo:        stringValue(data.TradeNo),
		Status:         data.Status,
		SubscribeId:    data.SubscribeID,
		SubscribeToken: stringValue(data.SubscribeToken),
		IsNew:          data.IsNew,
		CreatedAt:      data.CreatedAt,
		UpdatedAt:      data.UpdatedAt,
	}
}

func entOrdersToDetails(list []*ent.Order) []*Details {
	items := make([]*Details, 0, len(list))
	for _, item := range list {
		items = append(items, entToDetails(item))
	}
	return items
}

func entToDetails(data *ent.Order) *Details {
	if data == nil {
		return nil
	}
	return &Details{
		Id:             data.ID,
		ParentId:       data.ParentID,
		UserId:         data.UserID,
		OrderNo:        data.OrderNo,
		Type:           data.Type,
		Quantity:       data.Quantity,
		Price:          data.Price,
		Amount:         data.Amount,
		Discount:       data.Discount,
		Coupon:         stringValue(data.Coupon),
		CouponDiscount: data.CouponDiscount,
		PaymentId:      data.PaymentID,
		Method:         data.Method,
		FeeAmount:      data.FeeAmount,
		TradeNo:        stringValue(data.TradeNo),
		GiftAmount:     data.GiftAmount,
		Commission:     data.Commission,
		Status:         data.Status,
		SubscribeId:    data.SubscribeID,
		SubscribeToken: stringValue(data.SubscribeToken),
		IsNew:          data.IsNew,
		CreatedAt:      data.CreatedAt,
		UpdatedAt:      data.UpdatedAt,
	}
}

func entToPayment(data *ent.Payment) *paymentmodel.Payment {
	if data == nil {
		return nil
	}
	enable := data.Enable
	return &paymentmodel.Payment{
		Id:           data.ID,
		Name:         data.Name,
		Platform:     data.Platform,
		Icon:         data.Icon,
		Domain:       data.Domain,
		Config:       data.Config,
		Description:  data.Description,
		FeeMode:      data.FeeMode,
		FeePercent:   data.FeePercent,
		FeeAmount:    data.FeeAmount,
		Sort:         data.Sort,
		Enable:       &enable,
		Token:        data.Token,
		CurrencyUnit: data.CurrencyUnit,
		ExchangeRate: data.ExchangeRate,
		BillDesc:     data.BillDesc,
	}
}

func isBalancePayment(data *Details) bool {
	return data != nil && (data.PaymentId == balancePaymentID || data.Method == "balance")
}

func defaultBalancePayment() *paymentmodel.Payment {
	enable := true
	return &paymentmodel.Payment{
		Id:       balancePaymentID,
		Name:     "Balance",
		Platform: "balance",
		Enable:   &enable,
	}
}

func entToSubscribe(data *ent.Subscribe) *subscribemodel.Subscribe {
	if data == nil {
		return nil
	}
	show := data.Show
	sell := data.Sell
	allowDeduction := data.AllowDeduction
	renewalReset := data.RenewalReset
	return &subscribemodel.Subscribe{
		Id:                data.ID,
		Name:              data.Name,
		Language:          data.Language,
		Description:       data.Description,
		UnitPrice:         data.UnitPrice,
		UnitTime:          data.UnitTime,
		Discount:          data.Discount,
		Replacement:       data.Replacement,
		Inventory:         data.Inventory,
		Traffic:           data.Traffic,
		TrafficUnlimited:  data.TrafficUnlimited,
		SpeedLimit:        data.SpeedLimit,
		DeviceLimit:       data.DeviceLimit,
		Quota:             data.Quota,
		Nodes:             data.Nodes,
		NodeTags:          data.NodeTags,
		NodeGroupIds:      subscribemodel.JSONInt64Slice(data.NodeGroupIds),
		NodeGroupId:       data.NodeGroupID,
		TrafficLimit:      data.TrafficLimit,
		Show:              &show,
		Sell:              &sell,
		Sort:              data.Sort,
		DeductionRatio:    data.DeductionRatio,
		AllowDeduction:    &allowDeduction,
		ResetCycle:        data.ResetCycle,
		RenewalReset:      &renewalReset,
		ShowOriginalPrice: data.ShowOriginalPrice,
		CreatedAt:         data.CreatedAt,
		UpdatedAt:         data.UpdatedAt,
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
