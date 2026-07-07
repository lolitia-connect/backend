package node

import "github.com/perfect-panel/server/ent"

func (m *defaultServerModel) nodeCreate(data *Node) *ent.NodeCreate {
	return m.db.Node.Create().SetName(data.Name).SetTags(data.Tags).SetPort(data.Port).SetAddress(data.Address).SetServerID(data.ServerId).SetProtocol(data.Protocol).SetProtocolID(data.ProtocolId).SetNillableEnabled(data.Enabled).SetNodeType(data.NodeType).SetNillableIsHidden(data.IsHidden).SetSort(data.Sort).SetNodeGroupIds([]int64(data.NodeGroupIds))
}

func (m *defaultServerModel) nodeUpdate(data *Node) *ent.NodeUpdateOne {
	return m.db.Node.UpdateOneID(data.Id).SetName(data.Name).SetTags(data.Tags).SetPort(data.Port).SetAddress(data.Address).SetServerID(data.ServerId).SetProtocol(data.Protocol).SetProtocolID(data.ProtocolId).SetNillableEnabled(data.Enabled).SetNodeType(data.NodeType).SetNillableIsHidden(data.IsHidden).SetSort(data.Sort).SetNodeGroupIds([]int64(data.NodeGroupIds))
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
