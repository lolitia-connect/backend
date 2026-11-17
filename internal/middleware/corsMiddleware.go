package middleware

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func CorsMiddleware(c context.Context, ctx *app.RequestContext) {
	NewCorsMiddleware(nil)(c, ctx)
}

func NewCorsMiddleware(origins []string) app.HandlerFunc {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[strings.TrimSuffix(strings.ToLower(strings.TrimSpace(origin)), "/")] = true
	}
	return func(ctx context.Context, request *app.RequestContext) {
		origin := string(request.GetHeader("Origin"))
		if len(origins) > 0 {
			request.Header("Vary", "Origin")
		}
		if len(origins) == 0 || allowed[strings.ToLower(origin)] {
			if origin == "" {
				origin = "*"
			}
			request.Header("Access-Control-Allow-Origin", origin)
			request.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, UPDATE")
			request.Header("Access-Control-Allow-Headers", "Content-Type, Origin, X-CSRF-Token, Authorization, AccessToken, Token, Range, Idempotency-Key")
			request.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers")
			if origin != "*" {
				request.Header("Access-Control-Allow-Credentials", "true")
			}
			request.Header("Access-Control-Max-Age", "172800")
		}
		if string(request.Method()) == consts.MethodOptions {
			request.AbortWithStatus(consts.StatusNoContent)
			return
		}
		request.Next(ctx)
	}
}
