package user

import "github.com/perfect-panel/server/ent"

func userCreate(c *ent.UserCreate, data *User) *ent.UserCreate {
	if data.Id > 0 {
		c.SetID(data.Id)
	}
	return c.SetPassword(data.Password).
		SetAlgo(data.Algo).
		SetNillableSalt(stringPtr(data.Salt)).
		SetAvatar(data.Avatar).
		SetBalance(data.Balance).
		SetReferCode(data.ReferCode).
		SetRefererID(data.RefererId).
		SetCommission(data.Commission).
		SetReferralPercentage(data.ReferralPercentage).
		SetNillableOnlyFirstPurchase(data.OnlyFirstPurchase).
		SetGiftAmount(data.GiftAmount).
		SetNillableEnable(data.Enable).
		SetNillableIsAdmin(data.IsAdmin).
		SetNillableEnableBalanceNotify(data.EnableBalanceNotify).
		SetNillableEnableLoginNotify(data.EnableLoginNotify).
		SetNillableEnableSubscribeNotify(data.EnableSubscribeNotify).
		SetNillableEnableTradeNotify(data.EnableTradeNotify).
		SetRules(data.Rules).
		SetNillableDeletedAt(data.DeletedAt)
}

func userUpdate(u *ent.UserUpdateOne, data *User) *ent.UserUpdateOne {
	return u.SetPassword(data.Password).
		SetAlgo(data.Algo).
		SetNillableSalt(stringPtr(data.Salt)).
		SetAvatar(data.Avatar).
		SetBalance(data.Balance).
		SetReferCode(data.ReferCode).
		SetRefererID(data.RefererId).
		SetCommission(data.Commission).
		SetReferralPercentage(data.ReferralPercentage).
		SetNillableOnlyFirstPurchase(data.OnlyFirstPurchase).
		SetGiftAmount(data.GiftAmount).
		SetNillableEnable(data.Enable).
		SetNillableIsAdmin(data.IsAdmin).
		SetNillableEnableBalanceNotify(data.EnableBalanceNotify).
		SetNillableEnableLoginNotify(data.EnableLoginNotify).
		SetNillableEnableSubscribeNotify(data.EnableSubscribeNotify).
		SetNillableEnableTradeNotify(data.EnableTradeNotify).
		SetRules(data.Rules).
		SetNillableDeletedAt(data.DeletedAt)
}

func subscribeCreate(c *ent.UserSubscribeCreate, data *Subscribe) *ent.UserSubscribeCreate {
	if data.Id > 0 {
		c.SetID(data.Id)
	}
	return c.SetUserID(data.UserId).
		SetOrderID(data.OrderId).
		SetSubscribeID(data.SubscribeId).
		SetNodeGroupID(data.NodeGroupId).
		SetNillableGroupLocked(data.GroupLocked).
		SetStartTime(data.StartTime).
		SetExpireTime(data.ExpireTime).
		SetNillableFinishedAt(data.FinishedAt).
		SetTraffic(data.Traffic).
		SetTrafficUnlimited(data.TrafficUnlimited).
		SetDownload(data.Download).
		SetUpload(data.Upload).
		SetExpiredDownload(data.ExpiredDownload).
		SetExpiredUpload(data.ExpiredUpload).
		SetToken(data.Token).
		SetUUID(data.UUID).
		SetStatus(data.Status).
		SetNote(data.Note)
}

func subscribeUpdate(u *ent.UserSubscribeUpdateOne, data *Subscribe) *ent.UserSubscribeUpdateOne {
	return u.SetUserID(data.UserId).
		SetOrderID(data.OrderId).
		SetSubscribeID(data.SubscribeId).
		SetNodeGroupID(data.NodeGroupId).
		SetNillableGroupLocked(data.GroupLocked).
		SetStartTime(data.StartTime).
		SetExpireTime(data.ExpireTime).
		SetNillableFinishedAt(data.FinishedAt).
		SetTraffic(data.Traffic).
		SetTrafficUnlimited(data.TrafficUnlimited).
		SetDownload(data.Download).
		SetUpload(data.Upload).
		SetExpiredDownload(data.ExpiredDownload).
		SetExpiredUpload(data.ExpiredUpload).
		SetToken(data.Token).
		SetUUID(data.UUID).
		SetStatus(data.Status).
		SetNote(data.Note)
}

func authMethodCreate(c *ent.UserAuthMethodCreate, data *AuthMethods) *ent.UserAuthMethodCreate {
	if data.Id > 0 {
		c.SetID(data.Id)
	}
	return c.SetUserID(data.UserId).SetAuthType(data.AuthType).SetAuthIdentifier(data.AuthIdentifier).SetVerified(data.Verified)
}

func authMethodUpdate(u *ent.UserAuthMethodUpdateOne, data *AuthMethods) *ent.UserAuthMethodUpdateOne {
	return u.SetUserID(data.UserId).SetAuthType(data.AuthType).SetAuthIdentifier(data.AuthIdentifier).SetVerified(data.Verified)
}

func deviceCreate(c *ent.UserDeviceCreate, data *Device) *ent.UserDeviceCreate {
	if data.Id > 0 {
		c.SetID(data.Id)
	}
	return c.SetIP(data.Ip).
		SetUserID(data.UserId).
		SetNillableUserAgent(stringPtr(data.UserAgent)).
		SetIdentifier(data.Identifier).
		SetShortCode(data.ShortCode).
		SetOnline(data.Online).
		SetEnabled(data.Enabled)
}

func deviceUpdate(u *ent.UserDeviceUpdateOne, data *Device) *ent.UserDeviceUpdateOne {
	return u.SetIP(data.Ip).
		SetUserID(data.UserId).
		SetNillableUserAgent(stringPtr(data.UserAgent)).
		SetIdentifier(data.Identifier).
		SetShortCode(data.ShortCode).
		SetOnline(data.Online).
		SetEnabled(data.Enabled)
}
