package middleware

import (
	"context"

	"ariga.io/entcache"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func EntCacheMiddleware() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		if string(ctx.Method()) == consts.MethodGet {
			c = entcache.NewContext(c)
		}
		ctx.Next(c)
	}
}
