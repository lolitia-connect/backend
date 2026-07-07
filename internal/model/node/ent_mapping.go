package node

import "github.com/perfect-panel/server/ent"

func entToServer(data *ent.Server) *Server {
	if data == nil {
		return nil
	}
	protocols := ""
	if data.Protocols != nil {
		protocols = *data.Protocols
	}
	return &Server{
		Id:              data.ID,
		Name:            data.Name,
		Country:         data.Country,
		City:            data.City,
		Address:         data.Address,
		Sort:            data.Sort,
		Protocols:       protocols,
		LastReportedAt:  data.LastReportedAt,
		Longitude:       data.Longitude,
		Latitude:        data.Latitude,
		LongitudeCenter: data.LongitudeCenter,
		LatitudeCenter:  data.LatitudeCenter,
		CreatedAt:       data.CreatedAt,
		UpdatedAt:       data.UpdatedAt,
	}
}

func entServersToModel(list []*ent.Server) []*Server {
	items := make([]*Server, 0, len(list))
	for _, item := range list {
		items = append(items, entToServer(item))
	}
	return items
}

func entToNode(data *ent.Node) *Node {
	if data == nil {
		return nil
	}
	enabled := data.Enabled
	isHidden := data.IsHidden
	return &Node{
		Id:           data.ID,
		Name:         data.Name,
		Tags:         data.Tags,
		Port:         data.Port,
		Address:      data.Address,
		ServerId:     data.ServerID,
		Protocol:     data.Protocol,
		ProtocolId:   data.ProtocolID,
		Enabled:      &enabled,
		NodeType:     data.NodeType,
		IsHidden:     &isHidden,
		Sort:         data.Sort,
		NodeGroupIds: JSONInt64Slice(data.NodeGroupIds),
		CreatedAt:    data.CreatedAt,
		UpdatedAt:    data.UpdatedAt,
	}
}

func entNodesToModel(list []*ent.Node) []*Node {
	items := make([]*Node, 0, len(list))
	for _, item := range list {
		items = append(items, entToNode(item))
	}
	return items
}

func entToServerConfigOverride(data *ent.ServerConfigOverride) *ServerConfigOverride {
	if data == nil {
		return nil
	}
	return &ServerConfigOverride{
		Id:         data.ID,
		ServerId:   data.ServerID,
		IPStrategy: data.IPStrategy,
		DNS:        data.DNS,
		Block:      data.Block,
		Outbound:   data.Outbound,
		CreatedAt:  data.CreatedAt,
		UpdatedAt:  data.UpdatedAt,
	}
}
