package subscribe

import (
	"testing"

	"github.com/perfect-panel/server/internal/model/node"
)

func boolPtr(v bool) *bool { return &v }

func makeNode(id int64, protocol string, groupIds []int64, tags string) *node.Node {
	return &node.Node{
		Id:           id,
		Name:         "node-" + string(rune('A'+id-1)),
		Protocol:     protocol,
		Tags:         tags,
		Enabled:      boolPtr(true),
		IsHidden:     boolPtr(false),
		NodeGroupIds: node.JSONInt64Slice(groupIds),
	}
}

// ==================== collectGroupIds ====================

func TestCollectGroupIds_UserSubOverride(t *testing.T) {
	// 当 userSub.NodeGroupId != 0 时，用它作为主节点组，但仍合并备用节点组
	// userSub=5, primary=1, backup=[2,3,4] -> 主节点组 5 + 备用 [2,3,4] = [5,2,3,4]
	result := collectGroupIds(5, 1, []int64{2, 3, 4})
	if len(result) != 4 {
		t.Errorf("expected 4 groups [5,2,3,4], got %d: %v", len(result), result)
	}
	for i, expected := range []int64{5, 2, 3, 4} {
		if result[i] != expected {
			t.Errorf("result[%d] = %d, want %d", i, result[i], expected)
		}
	}
}

func TestCollectGroupIds_UserSubOverride_Dedup(t *testing.T) {
	// userSub=1, primary=1, backup=[1,2,3] -> 主节点组 1 + 备用 [2,3] = [1,2,3]
	result := collectGroupIds(1, 1, []int64{1, 2, 3})
	if len(result) != 3 {
		t.Errorf("expected 3 groups [1,2,3], got %d: %v", len(result), result)
	}
	for i, expected := range []int64{1, 2, 3} {
		if result[i] != expected {
			t.Errorf("result[%d] = %d, want %d", i, result[i], expected)
		}
	}
}

func TestCollectGroupIds_PrimaryAndBackup(t *testing.T) {
	// 主节点组 1 + 备用节点组 [2, 3, 4] -> [1, 2, 3, 4]
	result := collectGroupIds(0, 1, []int64{2, 3, 4})
	if len(result) != 4 {
		t.Errorf("expected 4 groups, got %d: %v", len(result), result)
	}
	for i, expected := range []int64{1, 2, 3, 4} {
		if result[i] != expected {
			t.Errorf("result[%d] = %d, want %d", i, result[i], expected)
		}
	}
}

func TestCollectGroupIds_Deduplication(t *testing.T) {
	// 主节点组 1 + 备用节点组 [1, 2, 3] -> 去重后 [1, 2, 3]
	result := collectGroupIds(0, 1, []int64{1, 2, 3})
	if len(result) != 3 {
		t.Errorf("expected 3 groups after dedup, got %d: %v", len(result), result)
	}
	for i, expected := range []int64{1, 2, 3} {
		if result[i] != expected {
			t.Errorf("result[%d] = %d, want %d", i, result[i], expected)
		}
	}
}

func TestCollectGroupIds_NoPrimaryGroup(t *testing.T) {
	// 没有主节点组，只有备用节点组
	result := collectGroupIds(0, 0, []int64{2, 3})
	if len(result) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(result), result)
	}
	for i, expected := range []int64{2, 3} {
		if result[i] != expected {
			t.Errorf("result[%d] = %d, want %d", i, result[i], expected)
		}
	}
}

func TestCollectGroupIds_OnlyPrimary(t *testing.T) {
	// 只有主节点组，没有备用节点组
	result := collectGroupIds(0, 1, nil)
	if len(result) != 1 || result[0] != 1 {
		t.Errorf("expected [1], got %v", result)
	}
}

func TestCollectGroupIds_Empty(t *testing.T) {
	// 什么都没配置
	result := collectGroupIds(0, 0, nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestCollectGroupIds_BackupDuplicates(t *testing.T) {
	// 备用节点组中有重复
	result := collectGroupIds(0, 1, []int64{2, 2, 3, 3, 4})
	if len(result) != 4 {
		t.Errorf("expected 4 groups, got %d: %v", len(result), result)
	}
	for i, expected := range []int64{1, 2, 3, 4} {
		if result[i] != expected {
			t.Errorf("result[%d] = %d, want %d", i, result[i], expected)
		}
	}
}

// ==================== filterNodesByProtocol ====================

func TestFilterNodesByProtocol(t *testing.T) {
	nodes := []*node.Node{
		makeNode(1, "vmess", []int64{1}, ""),
		makeNode(2, "VLESS", []int64{1}, ""),
		makeNode(3, "trojan", []int64{1}, ""),
		makeNode(4, "vless", []int64{1}, ""),
	}

	result := filterNodesByProtocol(nodes, "vless")
	if len(result) != 2 {
		t.Errorf("expected 2 vless nodes, got %d", len(result))
	}
	if result[0].Id != 2 || result[1].Id != 4 {
		t.Errorf("expected ids [2, 4], got [%d, %d]", result[0].Id, result[1].Id)
	}
}

func TestFilterNodesByProtocol_Empty(t *testing.T) {
	nodes := []*node.Node{
		makeNode(1, "vmess", []int64{1}, ""),
	}

	result := filterNodesByProtocol(nodes, "vless")
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestFilterNodesByProtocol_EmptyProtocol(t *testing.T) {
	nodes := []*node.Node{
		makeNode(1, "vmess", []int64{1}, ""),
		makeNode(2, "vless", []int64{1}, ""),
	}

	// 空协议不会匹配任何节点（调用方在 protocolType 为空时不会调用此函数）
	result := filterNodesByProtocol(nodes, "")
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

// ==================== mergeGroupAndPublicNodes ====================

func TestMergeGroupAndPublicNodes_Basic(t *testing.T) {
	groupNodes := []*node.Node{
		makeNode(1, "vmess", []int64{1}, ""),
		makeNode(2, "vless", []int64{1, 2}, ""),
	}
	publicNodes := []*node.Node{
		makeNode(3, "trojan", nil, ""),
	}

	result := mergeGroupAndPublicNodes(groupNodes, publicNodes)
	if len(result) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(result))
	}
	// 保持顺序：先分组节点，再公共节点
	if result[0].Id != 1 || result[1].Id != 2 || result[2].Id != 3 {
		t.Errorf("expected ids [1, 2, 3], got [%d, %d, %d]", result[0].Id, result[1].Id, result[2].Id)
	}
}

func TestMergeGroupAndPublicNodes_Deduplication(t *testing.T) {
	// 节点 1 同时出现在分组和公共中（不应发生，但应该能处理）
	groupNodes := []*node.Node{
		makeNode(1, "vmess", []int64{1}, "group-tag"),
		makeNode(2, "vless", []int64{1}, ""),
	}
	publicNodes := []*node.Node{
		makeNode(1, "vmess", nil, "public-tag"),
		makeNode(3, "trojan", nil, ""),
	}

	result := mergeGroupAndPublicNodes(groupNodes, publicNodes)
	if len(result) != 3 {
		t.Errorf("expected 3 nodes after dedup, got %d", len(result))
	}
	// 节点 1 应该保留分组版本（先出现的）
	if result[0].Tags != "group-tag" {
		t.Errorf("expected node 1 to have group-tag, got %s", result[0].Tags)
	}
}

func TestMergeGroupAndPublicNodes_BothEmpty(t *testing.T) {
	result := mergeGroupAndPublicNodes(nil, nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestMergeGroupAndPublicNodes_OnlyGroup(t *testing.T) {
	groupNodes := []*node.Node{
		makeNode(1, "vmess", []int64{1}, ""),
	}
	result := mergeGroupAndPublicNodes(groupNodes, nil)
	if len(result) != 1 {
		t.Errorf("expected 1, got %d", len(result))
	}
}

func TestMergeGroupAndPublicNodes_OnlyPublic(t *testing.T) {
	publicNodes := []*node.Node{
		makeNode(1, "vmess", nil, ""),
	}
	result := mergeGroupAndPublicNodes(nil, publicNodes)
	if len(result) != 1 {
		t.Errorf("expected 1, got %d", len(result))
	}
}

// ==================== 分组模式端到端逻辑测试 ====================

// TestGroupModeScenario_用户场景 模拟用户报告的场景：
// 节点 A 属于组 [1,2,3]，节点 B 属于组 [1,2,4]，套餐选择组 1
// 预期：A 和 B 都应该返回
func TestGroupModeScenario_UserReported(t *testing.T) {
	nodeA := makeNode(1, "vmess", []int64{1, 2, 3}, "")
	nodeB := makeNode(2, "vless", []int64{1, 2, 4}, "")

	// 模拟 FilterNodeList 的 SQL 逻辑：JSON_CONTAINS(node_group_ids, gid) OR ...
	// 对于 groupIds = [1]，节点 A [1,2,3] 和 B [1,2,4] 都包含 1，应该都匹配
	allGroupIds := []int64{1}
	allNodes := []*node.Node{nodeA, nodeB}

	var matched []*node.Node
	for _, n := range allNodes {
		for _, gid := range allGroupIds {
			for _, nid := range n.NodeGroupIds {
				if nid == gid {
					matched = append(matched, n)
					goto next
				}
			}
		}
	next:
	}

	if len(matched) != 2 {
		t.Errorf("expected 2 nodes for group 1, got %d", len(matched))
	}
}

// TestGroupModeScenario_MultipleGroups 模拟多分组场景：
// 主节点组 1，备用节点组 [2, 3]
// 节点 A 属于组 [1,2]，节点 B 属于组 [2,3]，节点 C 属于组 [3,4]，节点 D 属于组 [5]
// 预期：A（匹配组1,2）、B（匹配组2,3）、C（匹配组3）都返回，D 不返回
func TestGroupModeScenario_MultipleGroups(t *testing.T) {
	nodeA := makeNode(1, "vmess", []int64{1, 2}, "")
	nodeB := makeNode(2, "vless", []int64{2, 3}, "")
	nodeC := makeNode(3, "trojan", []int64{3, 4}, "")
	nodeD := makeNode(4, "shadowsocks", []int64{5}, "")

	// collectGroupIds: 主节点组 1 + 备用节点组 [2, 3] -> [1, 2, 3]
	allGroupIds := collectGroupIds(0, 1, []int64{2, 3})
	if len(allGroupIds) != 3 {
		t.Fatalf("expected 3 group ids, got %d", len(allGroupIds))
	}

	allNodes := []*node.Node{nodeA, nodeB, nodeC, nodeD}

	// 模拟 FilterNodeList 的匹配逻辑
	var groupNodes []*node.Node
	for _, n := range allNodes {
		for _, gid := range allGroupIds {
			for _, nid := range n.NodeGroupIds {
				if nid == gid {
					groupNodes = append(groupNodes, n)
					goto next
				}
			}
		}
	next:
	}

	if len(groupNodes) != 3 {
		t.Errorf("expected 3 group nodes, got %d", len(groupNodes))
	}

	// 合并公共节点（这里没有公共节点）
	publicNodes := []*node.Node{}
	result := mergeGroupAndPublicNodes(groupNodes, publicNodes)

	if len(result) != 3 {
		t.Errorf("expected 3 result nodes, got %d", len(result))
	}

	// 验证节点 ID
	ids := make(map[int64]bool)
	for _, n := range result {
		ids[n.Id] = true
	}
	if !ids[1] || !ids[2] || !ids[3] {
		t.Errorf("expected nodes 1,2,3, got ids: %v", ids)
	}
	if ids[4] {
		t.Errorf("node 4 should not be in result")
	}
}

// TestGroupModeScenario_PublicNodes 模拟公共节点场景：
// 节点 A 属于组 [1]，节点 B 没有分组（公共节点）
// 预期：A 和 B 都返回
func TestGroupModeScenario_PublicNodes(t *testing.T) {
	nodeA := makeNode(1, "vmess", []int64{1}, "")
	nodeB := makeNode(2, "vless", nil, "") // 公共节点

	allGroupIds := []int64{1}
	allNodes := []*node.Node{nodeA, nodeB}

	// 分组节点
	var groupNodes []*node.Node
	for _, n := range allNodes {
		for _, gid := range allGroupIds {
			for _, nid := range n.NodeGroupIds {
				if nid == gid {
					groupNodes = append(groupNodes, n)
					goto next
				}
			}
		}
	next:
	}

	// 公共节点
	var publicNodes []*node.Node
	for _, n := range allNodes {
		if len(n.NodeGroupIds) == 0 {
			publicNodes = append(publicNodes, n)
		}
	}

	if len(groupNodes) != 1 {
		t.Errorf("expected 1 group node, got %d", len(groupNodes))
	}
	if len(publicNodes) != 1 {
		t.Errorf("expected 1 public node, got %d", len(publicNodes))
	}

	result := mergeGroupAndPublicNodes(groupNodes, publicNodes)
	if len(result) != 2 {
		t.Errorf("expected 2 result nodes, got %d", len(result))
	}
}

// TestGroupModeScenario_FullFlow 模拟完整流程：
// 套餐配置：主节点组 1，备用节点组 [2]
// 节点 A [1,2,3]，节点 B [1,2,4]，节点 C [2]，节点 D [3]，节点 E []（公共）
// 预期：A、B、C（分组匹配）+ E（公共）= 4 个节点
func TestGroupModeScenario_FullFlow(t *testing.T) {
	nodeA := makeNode(1, "vmess", []int64{1, 2, 3}, "")
	nodeB := makeNode(2, "vless", []int64{1, 2, 4}, "")
	nodeC := makeNode(3, "trojan", []int64{2}, "")
	nodeD := makeNode(4, "shadowsocks", []int64{3}, "")
	nodeE := makeNode(5, "vmess", nil, "")

	// 收集分组 ID
	allGroupIds := collectGroupIds(0, 1, []int64{2})
	if len(allGroupIds) != 2 {
		t.Fatalf("expected 2 group ids [1,2], got %v", allGroupIds)
	}

	allNodes := []*node.Node{nodeA, nodeB, nodeC, nodeD, nodeE}

	// 分组节点
	var groupNodes []*node.Node
	for _, n := range allNodes {
		for _, gid := range allGroupIds {
			for _, nid := range n.NodeGroupIds {
				if nid == gid {
					groupNodes = append(groupNodes, n)
					goto next
				}
			}
		}
	next:
	}

	// 公共节点
	var publicNodes []*node.Node
	for _, n := range allNodes {
		if len(n.NodeGroupIds) == 0 {
			publicNodes = append(publicNodes, n)
		}
	}

	if len(groupNodes) != 3 {
		t.Errorf("expected 3 group nodes (A,B,C), got %d", len(groupNodes))
	}
	if len(publicNodes) != 1 {
		t.Errorf("expected 1 public node (E), got %d", len(publicNodes))
	}

	result := mergeGroupAndPublicNodes(groupNodes, publicNodes)
	if len(result) != 4 {
		t.Errorf("expected 4 result nodes, got %d", len(result))
	}

	ids := make(map[int64]bool)
	for _, n := range result {
		ids[n.Id] = true
	}
	if !ids[1] || !ids[2] || !ids[3] || !ids[5] {
		t.Errorf("expected nodes 1,2,3,5, got ids: %v", ids)
	}
	if ids[4] {
		t.Errorf("node 4 (only in group 3) should not be in result")
	}
}
