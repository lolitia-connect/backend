package edge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/edgeauth"
	"github.com/perfect-panel/server/internal/logic/edge"
	"github.com/perfect-panel/server/internal/svc"
	"go.uber.org/zap"
)

// ManifestHandler serves the private Edge Manifest contract. It does not use
// normal user authentication and does not emit the application's JSON envelope.
// A failed credential and an unknown user token deliberately share a 404 reply.
//
// @Summary Get Edge subscription manifest
// @Tags edge
// @Produce json
// @Param token query string true "Subscription token"
// @Param Authorization header string true "PPanel Edge HMAC credential"
// @Param X-Request-ID header string true "One-time UUID bound into the HMAC"
// @Success 200 {object} types.EdgeManifestResponse
// @Failure 404 {string} string
// @Router /api/edge/v1/manifest [get]
func ManifestHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		token := strings.TrimSpace(ctx.Query("token"))
		kid, valid := edgeauth.AuthenticateManifestRequest(string(ctx.GetHeader("Authorization")), token, string(ctx.GetHeader("X-Request-ID")), svcCtx.Config.EdgeSubscribe, time.Now())
		if !valid {
			ctx.String(consts.StatusNotFound, "Not Found")
			return
		}
		claimed, err := edgeauth.ClaimManifestRequest(c, svcCtx.Redis, kid, string(ctx.GetHeader("X-Request-ID")), svcCtx.Config.EdgeSubscribe)
		if err != nil {
			zap.S().Errorw("[Edge Manifest] replay protection unavailable", zap.String("error", err.Error()))
			ctx.String(consts.StatusServiceUnavailable, "Service Unavailable")
			return
		}
		if !claimed {
			ctx.String(consts.StatusNotFound, "Not Found")
			return
		}

		logic := edge.NewManifestLogic(c, svcCtx)
		response, err := logic.Manifest(token)
		if err != nil {
			if errors.Is(err, edge.ErrManifestNotFound) {
				ctx.String(consts.StatusNotFound, "Not Found")
				return
			}
			zap.S().Errorw("[Edge Manifest] build failed", zap.String("error", err.Error()))
			ctx.String(consts.StatusInternalServerError, "Internal Server Error")
			return
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.JSON(consts.StatusOK, response)
	}
}
