package log

import (
	loglogic "github.com/perfect-panel/server/internal/logic/admin/log"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

func FilterAdminActionLogHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(request *hertzx.Context) {
		var params types.FilterAdminActionLogRequest
		if err := request.ShouldBind(&params); err != nil {
			result.ParamErrorResult(request, err)
			return
		}
		if err := service.Validate(&params); err != nil {
			result.ParamErrorResult(request, err)
			return
		}
		response, err := loglogic.FilterAdminActionLog(request.Request.Context(), service, &params)
		result.HttpResult(request, response, err)
	}
}

func FilterUnmatchedPaymentLogHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(request *hertzx.Context) {
		var params types.FilterUnmatchedPaymentLogRequest
		if err := request.ShouldBind(&params); err != nil {
			result.ParamErrorResult(request, err)
			return
		}
		if err := service.Validate(&params); err != nil {
			result.ParamErrorResult(request, err)
			return
		}
		response, err := loglogic.FilterUnmatchedPaymentLog(request.Request.Context(), service, &params)
		result.HttpResult(request, response, err)
	}
}
