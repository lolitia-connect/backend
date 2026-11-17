package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	serverv1 "github.com/perfect-panel/server/api/server/v1"
	serverlogic "github.com/perfect-panel/server/internal/logic/server"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
)

type serverDevices interface {
	GetServerAliveList(context.Context, *types.GetServerUserListRequest) (*types.GetServerAliveListResponse, error)
	ClaimServerDevice(context.Context, *types.ClaimServerDeviceRequest) (*types.ClaimServerDeviceResponse, error)
}

func GetServerAliveListHandler(service *svc.ServiceContext) app.HandlerFunc {
	return getServerAliveListHandler(service, serverlogic.DeviceService{Service: service})
}

func getServerAliveListHandler(service *svc.ServiceContext, devices serverDevices) app.HandlerFunc {
	return func(ctx context.Context, request *app.RequestContext) {
		common, err := serverCommonRequest(request)
		if err != nil {
			writeParamError(request, err)
			return
		}
		query := &types.GetServerUserListRequest{ServerCommon: common}
		if err := service.Validate(query); err != nil {
			writeParamError(request, err)
			return
		}
		response, err := devices.GetServerAliveList(ctx, query)
		if err != nil {
			writeServerReportResult(request, err)
			return
		}
		request.Header("Cache-Control", "no-store")
		if acceptsProtobuf(request) {
			writeServerProtobuf(request, consts.StatusOK, &serverv1.GetServerAliveListResponse{Code: 200, Message: "success", Data: &serverv1.ServerAliveListData{Alive: response.Alive}})
			return
		}
		request.JSON(consts.StatusOK, response)
	}
}

func ClaimServerDeviceHandler(service *svc.ServiceContext) app.HandlerFunc {
	return claimServerDeviceHandler(service, serverlogic.DeviceService{Service: service})
}

func claimServerDeviceHandler(service *svc.ServiceContext, devices serverDevices) app.HandlerFunc {
	return func(ctx context.Context, request *app.RequestContext) {
		query := &types.ClaimServerDeviceRequest{}
		if isProtobufRequest(request) {
			var message serverv1.ClaimServerDeviceRequest
			if err := request.BindProtobuf(&message); err != nil {
				writeParamError(request, err)
				return
			}
			query.SID, query.IP = message.UserId, message.Ip
		} else if err := request.BindJSON(query); err != nil {
			writeParamError(request, err)
			return
		}
		common, err := serverCommonRequest(request)
		if err != nil {
			writeParamError(request, err)
			return
		}
		query.ServerCommon = common
		if err := service.Validate(query); err != nil {
			writeParamError(request, err)
			return
		}
		response, err := devices.ClaimServerDevice(ctx, query)
		if err != nil {
			writeServerReportResult(request, err)
			return
		}
		request.Header("Cache-Control", "no-store")
		if acceptsProtobuf(request) {
			writeServerProtobuf(request, consts.StatusOK, &serverv1.ClaimServerDeviceResponse{Code: 200, Message: "success", Data: &serverv1.ServerDeviceClaimData{Allowed: response.Allowed, TtlSeconds: response.TTLSeconds}})
			return
		}
		request.JSON(consts.StatusOK, response)
	}
}
