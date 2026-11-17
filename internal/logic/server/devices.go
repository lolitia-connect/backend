package server

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/xerr"
)

type DeviceService struct{ Service *svc.ServiceContext }

func (service DeviceService) GetServerAliveList(ctx context.Context, request *types.GetServerUserListRequest) (*types.GetServerAliveListResponse, error) {
	if request.ServerId <= 0 || request.Protocol == "" || request.ProtocolId == "" {
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	index, err := servedUserIndex(ctx, service.Service, request.ServerId, request.Protocol, request.ProtocolId)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(index.IDs))
	for subscriptionID := range index.IDs {
		ids = append(ids, subscriptionID)
	}
	counts, err := node.CountDeviceLeases(ctx, service.Service.Redis, ids)
	if err != nil {
		return nil, err
	}
	return &types.GetServerAliveListResponse{Alive: counts}, nil
}

func (service DeviceService) ClaimServerDevice(ctx context.Context, request *types.ClaimServerDeviceRequest) (*types.ClaimServerDeviceResponse, error) {
	indexes := service.Service.NodeUserIndexes()
	if !indexes.AcquireClaim() {
		return nil, node.ErrServerBusy
	}
	defer indexes.ReleaseClaim()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	address, err := netip.ParseAddr(request.IP)
	if err != nil || address.Zone() != "" || address.IsUnspecified() || request.SID <= 0 || request.ServerId <= 0 || request.Protocol == "" || request.ProtocolId == "" {
		return nil, xerr.NewErrCode(xerr.InvalidParams)
	}
	for attempt := 0; attempt < 3; attempt++ {
		index, err := servedUserIndex(ctx, service.Service, request.ServerId, request.Protocol, request.ProtocolId)
		if err != nil {
			return nil, err
		}
		limit, served := index.Limits[request.SID]
		if !served {
			current, err := node.ServerCacheGeneration(ctx, service.Service.Redis, request.ServerId)
			if err != nil {
				return nil, err
			}
			if current != index.Generation {
				continue
			}
			return nil, xerr.NewErrCode(xerr.InvalidAccess)
		}
		decision, err := node.ClaimDeviceLease(ctx, service.Service.Redis, request.ServerId, request.SID, address.Unmap().String(), limit, index.Generation)
		if err != nil {
			return nil, fmt.Errorf("device lease: %w", err)
		}
		if decision < 0 {
			continue
		}
		return &types.ClaimServerDeviceResponse{Allowed: decision == 1, TTLSeconds: int64(node.Expiry.Seconds())}, nil
	}
	return nil, node.ErrServerBusy
}
