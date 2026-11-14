package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// Get user list
//
// @Summary Get user list
// @Tags node
// @Accept json
// @Produce json,application/protobuf
// @Security NodeSecret
// @Param request query types.GetServerUserListRequest false "Request parameters"
// @Success 200 {object} types.GetServerUserListResponse
// @Router /v1/server/user [get]
func GetServerUserListHandler(svcCtx *svc.ServiceContext) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		acceptsProtobuf := acceptsProtobuf(ctx)
		ctx.Header("Vary", "Accept")
		commonReq, err := serverCommonRequest(ctx)
		if err != nil {
			writeParamError(ctx, err)
			return
		}
		req := types.GetServerUserListRequest{ServerCommon: commonReq}
		if validateErr := svcCtx.Validate(&req); validateErr != nil {
			writeParamError(ctx, validateErr)
			return
		}

		ifNoneMatch := string(ctx.GetHeader("If-None-Match"))
		l := server.NewGetServerUserListLogic(c, svcCtx, server.RequestMeta{
			IfNoneMatch: ifNoneMatchForRepresentation(ifNoneMatch, acceptsProtobuf),
		})
		resp, err := l.GetServerUserList(&req)
		writeHeaders(ctx, l.ResponseMeta().Headers)
		if err != nil {
			if errors.Is(err, xerr.StatusNotModified) {
				ctx.String(consts.StatusNotModified, "Not Modified")
				return
			}
			writeServerText(ctx, consts.StatusNotFound, "Not Found")
			return
		}
		if acceptsProtobuf {
			if err := writeServerProtobufWithETag(ctx, serverUserListResponseToProtobuf(resp), ifNoneMatch); err != nil {
				writeServerReportResult(ctx, err)
			}
			return
		}
		ctx.JSON(consts.StatusOK, resp)
	}
}
