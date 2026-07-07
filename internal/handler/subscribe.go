package handler

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func SubscribeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := types.SubscribeRequest{
			Token:  string(ctx.GetHeader("token")),
			UA:     string(ctx.UserAgent()),
			Type:   ctx.Query("type"),
			Agent:  ctx.Query("agent"),
			Params: getQueryMap(ctx),
		}
		// 筛选协议类型，只允许支持的协议
		req.Type = filterSupportedProtocol(req.Type)
		if req.Token == "" {
			req.Token = ctx.Query("token")
		}

		if svcCtx.Config.Subscribe.PanDomain {
			domainArr := strings.Split(string(ctx.Host()), ".")
			if len(domainArr) == 0 {
				ctx.String(consts.StatusForbidden, "Access denied")
				return
			}
			short, err := tool.FixedUniqueString(req.Token, 8, "")
			if err != nil {
				zap.S().Errorf("[SubscribeHandler] Generate short token failed: %v", err)
				ctx.String(consts.StatusInternalServerError, "Internal Server")
				return
			}
			if strings.ToLower(short) != strings.ToLower(domainArr[0]) {
				zap.S().Debugf("[SubscribeHandler] short token mismatch, short: %s, domain: %s", short, domainArr[0])
				ctx.String(consts.StatusForbidden, "Access denied")
				return
			}
		}

		if svcCtx.Config.Subscribe.UserAgentLimit && !subscribe.IsUserAgentAllowed(c, svcCtx, req.UA) {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}
		writeSubscribeResponse(c, ctx, svcCtx, req)
	}
}

func PanDomainSubscribeHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		ua := string(ctx.UserAgent())
		if svcCtx.Config.Subscribe.UserAgentLimit && !subscribe.IsUserAgentAllowed(c, svcCtx, ua) {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}

		domainArr := strings.Split(string(ctx.Host()), ".")
		if len(domainArr) < 2 {
			ctx.String(consts.StatusForbidden, "Access denied")
			return
		}

		writeSubscribeResponse(c, ctx, svcCtx, types.SubscribeRequest{
			Token:  domainArr[0],
			UA:     ua,
			Type:   filterSupportedProtocol(ctx.Query("type")),
			Agent:  ctx.Query("agent"),
			Params: getQueryMap(ctx),
		})
	}
}

func writeSubscribeResponse(c context.Context, ctx *app.RequestContext, svcCtx *svc.ServiceContext, req types.SubscribeRequest) {
	l := subscribe.NewSubscribeLogic(c, svcCtx, subscribe.RequestMeta{
		Host:       string(ctx.Host()),
		RequestURI: string(ctx.URI().RequestURI()),
		UserAgent:  string(ctx.UserAgent()),
		ClientIP:   ctx.ClientIP(),
	})
	resp, err := l.Handler(&req)
	if err != nil {
		statusCode := consts.StatusInternalServerError
		errMsg := "Internal Server Error"
		var e *xerr.CodeError
		if errors.As(errors.Cause(err), &e) {
			switch e.GetErrCode() {
			case xerr.SubscribeNotAvailable:
				statusCode = consts.StatusNotFound
				errMsg = "Not Found"
			case xerr.SubscribeExpired:
				statusCode = consts.StatusForbidden
				errMsg = "Forbidden"
			}
		}
		ctx.String(statusCode, errMsg)
		return
	}
	for key, value := range resp.Headers {
		ctx.Header(key, value)
	}
	ctx.Header("subscription-userinfo", resp.Header)
	ctx.Data(consts.StatusOK, "text/plain; charset=utf-8", resp.Config)
}

// supportedProtocols 已支持的协议类型集合
var supportedProtocols = map[string]bool{
	string(config.Shadowsocks): true,
	string(config.Trojan):      true,
	string(config.Vmess):       true,
	string(config.Vless):       true,
	string(config.Hysteria):    true,
	string(config.Tuic):        true,
	string(config.AnyTLS):      true,
	string(config.Socks):       true,
	string(config.Naive):       true,
	string(config.HTTP):        true,
	string(config.Mieru):       true,
}

// filterSupportedProtocol 筛选协议类型，只返回支持的协议，不支持的返回空字符串
func filterSupportedProtocol(protocolType string) string {
	if protocolType == "" {
		return ""
	}
	if supportedProtocols[strings.ToLower(protocolType)] {
		return strings.ToLower(protocolType)
	}
	return ""
}

func getQueryMap(ctx *app.RequestContext) map[string]string {
	result := make(map[string]string)
	ctx.QueryArgs().VisitAll(func(key, value []byte) {
		k := string(key)
		if _, ok := result[k]; !ok {
			result[k] = string(value)
		}
	})
	return result
}
