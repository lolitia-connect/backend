package log

import (
	"context"
	"encoding/json"

	modellog "github.com/perfect-panel/server/internal/model/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/requestmeta"
)

func operatorLogPage(ctx context.Context, service *svc.ServiceContext, params types.FilterLogParams, userID int64, kind modellog.Type) ([]*modellog.SystemLog, int64, error) {
	return service.Store.Log().FilterSystemLog(ctx, &modellog.FilterParams{
		Page: params.Page, Size: params.Size, Type: kind.Uint8(), ObjectID: userID,
		Data: params.Date, StartDate: params.StartDate, EndDate: params.EndDate, Search: params.Search,
	})
}

func FilterAdminActionLog(ctx context.Context, service *svc.ServiceContext, request *types.FilterAdminActionLogRequest) (*types.FilterAdminActionLogResponse, error) {
	rows, total, err := operatorLogPage(ctx, service, request.FilterLogParams, request.UserId, modellog.TypeAdminAction)
	if err != nil {
		return nil, err
	}
	response := &types.FilterAdminActionLogResponse{Total: total, List: make([]types.AdminActionLog, 0, len(rows))}
	for _, row := range rows {
		var action modellog.AdminAction
		if err := json.Unmarshal([]byte(row.Content), &action); err != nil {
			continue
		}
		response.List = append(response.List, types.AdminActionLog{
			Metadata: requestmeta.Normalize(action.Metadata), Id: row.Id, UserId: row.ObjectID,
			Action: action.Action, Object: action.Object, ObjectId: action.ObjectID,
			Detail: requestmeta.Bound(action.Detail, 2048), Source: action.Source,
			TelegramSenderId: action.TelegramSenderID, Timestamp: action.Timestamp, CreatedAt: row.CreatedAt.UnixMilli(),
		})
	}
	return response, nil
}

func FilterUnmatchedPaymentLog(ctx context.Context, service *svc.ServiceContext, request *types.FilterUnmatchedPaymentLogRequest) (*types.FilterUnmatchedPaymentLogResponse, error) {
	rows, total, err := operatorLogPage(ctx, service, request.FilterLogParams, request.UserId, modellog.TypeUnmatchedPayment)
	if err != nil {
		return nil, err
	}
	response := &types.FilterUnmatchedPaymentLogResponse{Total: total, List: make([]types.UnmatchedPaymentLog, 0, len(rows))}
	for _, row := range rows {
		var payment modellog.UnmatchedPayment
		if err := json.Unmarshal([]byte(row.Content), &payment); err != nil {
			continue
		}
		payment.Normalize()
		response.List = append(response.List, types.UnmatchedPaymentLog{
			Metadata: payment.Metadata,
			Id:       row.Id, UserId: row.ObjectID, OrderNo: payment.OrderNo, TradeNo: payment.TradeNo,
			Platform: payment.Platform, Amount: payment.Amount, Currency: payment.Currency, Reason: payment.Reason,
			Timestamp: row.CreatedAt.UnixMilli(), CreatedAt: row.CreatedAt.UnixMilli(),
		})
	}
	return response, nil
}
