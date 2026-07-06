package subscribe

import "github.com/perfect-panel/server/ent"

func entToSubscribe(data *ent.Subscribe) *Subscribe {
	if data == nil {
		return nil
	}
	show := data.Show
	sell := data.Sell
	allowDeduction := data.AllowDeduction
	renewalReset := data.RenewalReset
	return &Subscribe{
		Id:                data.ID,
		Name:              data.Name,
		Language:          data.Language,
		Description:       data.Description,
		UnitPrice:         data.UnitPrice,
		UnitTime:          data.UnitTime,
		Discount:          data.Discount,
		Replacement:       data.Replacement,
		Inventory:         data.Inventory,
		Traffic:           data.Traffic,
		TrafficUnlimited:  data.TrafficUnlimited,
		SpeedLimit:        data.SpeedLimit,
		DeviceLimit:       data.DeviceLimit,
		Quota:             data.Quota,
		Nodes:             data.Nodes,
		NodeTags:          data.NodeTags,
		NodeGroupIds:      JSONInt64Slice(data.NodeGroupIds),
		NodeGroupId:       data.NodeGroupID,
		TrafficLimit:      data.TrafficLimit,
		Show:              &show,
		Sell:              &sell,
		Sort:              data.Sort,
		DeductionRatio:    data.DeductionRatio,
		AllowDeduction:    &allowDeduction,
		ResetCycle:        data.ResetCycle,
		RenewalReset:      &renewalReset,
		ShowOriginalPrice: data.ShowOriginalPrice,
		CreatedAt:         data.CreatedAt,
		UpdatedAt:         data.UpdatedAt,
	}
}

func entSubscribesToModel(list []*ent.Subscribe) []*Subscribe {
	items := make([]*Subscribe, 0, len(list))
	for _, item := range list {
		items = append(items, entToSubscribe(item))
	}
	return items
}

func entToGroup(data *ent.SubscribeGroup) *Group {
	if data == nil {
		return nil
	}
	return &Group{
		Id:          data.ID,
		Name:        data.Name,
		Description: data.Description,
		CreatedAt:   data.CreatedAt,
		UpdatedAt:   data.UpdatedAt,
	}
}

func entGroupsToModel(list []*ent.SubscribeGroup) []*Group {
	items := make([]*Group, 0, len(list))
	for _, item := range list {
		items = append(items, entToGroup(item))
	}
	return items
}
