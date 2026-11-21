package nekoproxy

import (
	"github.com/perfect-panel/server/internal/logic/admin/nekoproxy"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/hertzx"
	"github.com/perfect-panel/server/pkg/result"
)

func GetConfigHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(c *hertzx.Context) {
		resp, err := nekoproxy.NewLogic(c.Request.Context(), service).Config()
		result.HttpResult(c, resp, err)
	}
}

func UpdateConfigHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.UpdateNekoproxyConfigRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := service.Validate(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		err := nekoproxy.NewLogic(c.Request.Context(), service).UpdateConfig(&req)
		result.HttpResult(c, nil, err)
	}
}

func GetStateHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(c *hertzx.Context) {
		resp, err := nekoproxy.NewLogic(c.Request.Context(), service).State()
		result.HttpResult(c, resp, err)
	}
}

func GetBindingHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.GetNekoproxyBindingRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := service.Validate(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		resp, err := nekoproxy.NewLogic(c.Request.Context(), service).NodeEntry(req.NodeID)
		result.HttpResult(c, resp, err)
	}
}

func BindHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.BindNekoproxyRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := service.Validate(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		err := nekoproxy.NewLogic(c.Request.Context(), service).Bind(&req)
		result.HttpResult(c, nil, err)
	}
}

func UnbindHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.UnbindNekoproxyRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := service.Validate(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		err := nekoproxy.NewLogic(c.Request.Context(), service).Unbind(&req)
		result.HttpResult(c, nil, err)
	}
}

func SyncHandler(service *svc.ServiceContext) func(*hertzx.Context) {
	return func(c *hertzx.Context) {
		var req types.SyncNekoproxyRequest
		if err := c.ShouldBind(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		if err := service.Validate(&req); err != nil {
			result.ParamErrorResult(c, err)
			return
		}
		resp, err := nekoproxy.NewLogic(c.Request.Context(), service).Sync(&req)
		result.HttpResult(c, resp, err)
	}
}
