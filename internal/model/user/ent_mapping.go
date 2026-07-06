package user

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/server/ent"
	entauth "github.com/perfect-panel/server/ent/userauthmethod"
	entdevice "github.com/perfect-panel/server/ent/userdevice"
	entrecord "github.com/perfect-panel/server/ent/userdeviceonlinerecord"
	entsub "github.com/perfect-panel/server/ent/usersubscribe"
	entwithdrawal "github.com/perfect-panel/server/ent/userwithdrawal"
	"github.com/redis/go-redis/v9"
)

func getJSONCache(ctx context.Context, rds *redis.Client, key string, dest any) error {
	if rds == nil {
		return redis.Nil
	}
	data, err := rds.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func setJSONCache(ctx context.Context, rds *redis.Client, key string, value any) error {
	if rds == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return rds.Set(ctx, key, data, 0).Err()
}

func entToUser(e *ent.User) *User {
	if e == nil {
		return nil
	}
	return &User{
		Id:                    e.ID,
		Password:              e.Password,
		Algo:                  e.Algo,
		Salt:                  stringValue(e.Salt),
		Avatar:                e.Avatar,
		Balance:               e.Balance,
		ReferCode:             e.ReferCode,
		RefererId:             e.RefererID,
		Commission:            e.Commission,
		ReferralPercentage:    e.ReferralPercentage,
		OnlyFirstPurchase:     boolPtr(e.OnlyFirstPurchase),
		GiftAmount:            e.GiftAmount,
		Enable:                boolPtr(e.Enable),
		IsAdmin:               boolPtr(e.IsAdmin),
		EnableBalanceNotify:   boolPtr(e.EnableBalanceNotify),
		EnableLoginNotify:     boolPtr(e.EnableLoginNotify),
		EnableSubscribeNotify: boolPtr(e.EnableSubscribeNotify),
		EnableTradeNotify:     boolPtr(e.EnableTradeNotify),
		Rules:                 e.Rules,
		CreatedAt:             e.CreatedAt,
		UpdatedAt:             e.UpdatedAt,
		DeletedAt:             e.DeletedAt,
	}
}

func entUsersToModels(items []*ent.User) []*User {
	out := make([]*User, 0, len(items))
	for _, item := range items {
		out = append(out, entToUser(item))
	}
	return out
}

func entToSubscribe(e *ent.UserSubscribe) *Subscribe {
	if e == nil {
		return nil
	}
	return &Subscribe{
		Id:               e.ID,
		UserId:           e.UserID,
		OrderId:          e.OrderID,
		SubscribeId:      e.SubscribeID,
		NodeGroupId:      e.NodeGroupID,
		GroupLocked:      boolPtr(e.GroupLocked),
		StartTime:        e.StartTime,
		ExpireTime:       e.ExpireTime,
		FinishedAt:       e.FinishedAt,
		Traffic:          e.Traffic,
		TrafficUnlimited: e.TrafficUnlimited,
		Download:         e.Download,
		Upload:           e.Upload,
		ExpiredDownload:  e.ExpiredDownload,
		ExpiredUpload:    e.ExpiredUpload,
		Token:            e.Token,
		UUID:             e.UUID,
		Status:           e.Status,
		Note:             e.Note,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}
}

func entSubscribesToModels(items []*ent.UserSubscribe) []*Subscribe {
	out := make([]*Subscribe, 0, len(items))
	for _, item := range items {
		out = append(out, entToSubscribe(item))
	}
	return out
}

func entToSubscribeDetails(e *ent.UserSubscribe) *SubscribeDetails {
	if e == nil {
		return nil
	}
	return &SubscribeDetails{
		Id:               e.ID,
		UserId:           e.UserID,
		OrderId:          e.OrderID,
		SubscribeId:      e.SubscribeID,
		NodeGroupId:      e.NodeGroupID,
		GroupLocked:      boolPtr(e.GroupLocked),
		StartTime:        e.StartTime,
		ExpireTime:       e.ExpireTime,
		FinishedAt:       e.FinishedAt,
		Traffic:          e.Traffic,
		TrafficUnlimited: e.TrafficUnlimited,
		Download:         e.Download,
		Upload:           e.Upload,
		Token:            e.Token,
		UUID:             e.UUID,
		Status:           e.Status,
		Note:             e.Note,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        e.UpdatedAt,
	}
}

func entSubscribeDetailsToModels(items []*ent.UserSubscribe) []*SubscribeDetails {
	out := make([]*SubscribeDetails, 0, len(items))
	for _, item := range items {
		out = append(out, entToSubscribeDetails(item))
	}
	return out
}

func entToAuthMethod(e *ent.UserAuthMethod) *AuthMethods {
	if e == nil {
		return nil
	}
	return &AuthMethods{Id: e.ID, UserId: e.UserID, AuthType: e.AuthType, AuthIdentifier: e.AuthIdentifier, Verified: e.Verified, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

func entAuthMethodsToModels(items []*ent.UserAuthMethod) []AuthMethods {
	out := make([]AuthMethods, 0, len(items))
	for _, item := range items {
		if model := entToAuthMethod(item); model != nil {
			out = append(out, *model)
		}
	}
	return out
}

func entAuthMethodsToPtrModels(items []*ent.UserAuthMethod) []*AuthMethods {
	out := make([]*AuthMethods, 0, len(items))
	for _, item := range items {
		out = append(out, entToAuthMethod(item))
	}
	return out
}

func entToDevice(e *ent.UserDevice) *Device {
	if e == nil {
		return nil
	}
	return &Device{Id: e.ID, Ip: e.IP, UserId: e.UserID, UserAgent: stringValue(e.UserAgent), Identifier: e.Identifier, ShortCode: e.ShortCode, Online: e.Online, Enabled: e.Enabled, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

func entDevicesToModels(items []*ent.UserDevice) []Device {
	out := make([]Device, 0, len(items))
	for _, item := range items {
		if model := entToDevice(item); model != nil {
			out = append(out, *model)
		}
	}
	return out
}

func entDevicesToPtrModels(items []*ent.UserDevice) []*Device {
	out := make([]*Device, 0, len(items))
	for _, item := range items {
		out = append(out, entToDevice(item))
	}
	return out
}

func entToDeviceOnlineRecord(e *ent.UserDeviceOnlineRecord) *DeviceOnlineRecord {
	if e == nil {
		return nil
	}
	return &DeviceOnlineRecord{Id: e.ID, UserId: e.UserID, Identifier: e.Identifier, OnlineTime: e.OnlineTime, OfflineTime: e.OfflineTime, OnlineSeconds: e.OnlineSeconds, DurationDays: e.DurationDays, CreatedAt: e.CreatedAt}
}

func boolPtr(v bool) *bool { return &v }

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func int64ToUint8(values []int64) []uint8 {
	out := make([]uint8, 0, len(values))
	for _, value := range values {
		out = append(out, uint8(value))
	}
	return out
}

func timePtr(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	return &v
}

var (
	_ = entsub.Table
	_ = entauth.Table
	_ = entdevice.Table
	_ = entrecord.Table
	_ = entwithdrawal.Table
)
