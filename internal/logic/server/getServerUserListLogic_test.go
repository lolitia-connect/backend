package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofrs/uuid/v5"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/model/subscribe"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/redis/go-redis/v9"
)

type reportNodes struct {
	node.Model
	protocol string
	users    node.OnlineUserSubscribe
}

func (nodes *reportNodes) FindOneServer(context.Context, int64) (*node.Server, error) {
	return &node.Server{Id: 4, Protocols: `[{"type":"vless","id":"1","enable":true},{"type":"vless","id":"2","enable":true}]`}, nil
}

func (nodes *reportNodes) UpdateOnlineUserSubscribe(_ context.Context, _ int64, protocol string, users node.OnlineUserSubscribe) error {
	nodes.protocol, nodes.users = protocol, users
	return nil
}

func (nodes *reportNodes) UpdateOnlineUserSubscribeGlobal(context.Context, node.OnlineUserSubscribe) error {
	return nil
}

type reportUsers struct{ user.Model }

func (reportUsers) FindSubscribesByIds(context.Context, []int64) ([]*user.Subscribe, error) {
	return []*user.Subscribe{{Id: 1, UserId: 5, UUID: "real-user"}, {Id: 7, UserId: 5, UUID: "real-seven"}, {Id: 8, UserId: 5, UUID: "real-eight"}}, nil
}

func (reportUsers) FindUsersByIds(context.Context, []int64) ([]*user.User, error) {
	enabled := true
	return []*user.User{{Id: 5, Enable: &enabled}}, nil
}

type reportStore struct {
	repository.Store
	nodes *reportNodes
}

func (store reportStore) Node() node.Model { return store.nodes }
func (store reportStore) User() user.Model { return reportUsers{} }

func TestOnlineReportsUseProtocolInstanceScope(t *testing.T) {
	ctx := context.Background()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	nodes := &reportNodes{}
	service := &svc.ServiceContext{Store: reportStore{nodes: nodes}, Redis: client}
	for protocolID, users := range map[string][]types.ServerUser{
		"1": {{Id: 1, UUID: "placeholder"}, {Id: 7, UUID: "real-seven"}},
		"2": {{Id: 8, UUID: "real-eight"}},
	} {
		data, err := json.Marshal(types.GetServerUserListResponse{Users: users})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Set(ctx, fmt.Sprintf("%s4:vless:%s", node.ServerUserListCacheKey, protocolID), data, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	request := &types.OnlineUsersRequest{ServerCommon: types.ServerCommon{ServerId: 4, Protocol: "vless", ProtocolId: "1"}, Users: []types.OnlineUser{
		{SID: 7, IP: "192.0.2.1"}, {SID: 7, IP: "::ffff:192.0.2.1"}, {SID: 8, IP: "192.0.2.2"}, {SID: 1, IP: "192.0.2.3"},
	}}
	logic := NewPushOnlineUsersLogic(ctx, service)
	if err := logic.PushOnlineUsers(request); err != nil {
		t.Fatal(err)
	}
	if nodes.protocol != "vless:1" || len(nodes.users) != 1 || len(nodes.users[7]) != 1 || nodes.users[7][0] != "192.0.2.1" {
		t.Fatalf("unscoped or duplicate online data: %s %+v", nodes.protocol, nodes.users)
	}
	request.ProtocolId = "2"
	if err := logic.PushOnlineUsers(request); err != nil {
		t.Fatal(err)
	}
	if len(nodes.users) != 1 || len(nodes.users[8]) != 1 {
		t.Fatalf("instance two scope: %+v", nodes.users)
	}
	request.Users = nil
	if err := logic.PushOnlineUsers(request); err != nil || len(nodes.users) != 0 {
		t.Fatalf("empty snapshot: %+v, %v", nodes.users, err)
	}
	request.ProtocolId = "missing"
	if err := logic.PushOnlineUsers(request); err == nil {
		t.Fatal("unknown instance accepted")
	}
	request.ProtocolId = "1"
	request.Users = []types.OnlineUser{{SID: 7, IP: "invalid"}}
	if err := logic.PushOnlineUsers(request); err == nil {
		t.Fatal("invalid online address accepted")
	}
}

func TestPlaceholderServerUserIsStable(t *testing.T) {
	first := placeholderServerUser(1, "vless", "secret")
	second := placeholderServerUser(1, "vless", "secret")
	otherProtocol := placeholderServerUser(1, "trojan", "secret")
	otherSecret := placeholderServerUser(1, "vless", "other-secret")

	if first.Id != 1 {
		t.Fatalf("placeholder user id = %d, want 1", first.Id)
	}
	if first.UUID == "" {
		t.Fatal("placeholder uuid is empty")
	}
	if _, err := uuid.FromString(first.UUID); err != nil {
		t.Fatalf("placeholder uuid is invalid: %v", err)
	}
	if first.UUID != second.UUID {
		t.Fatalf("placeholder uuid changed for same input: %s != %s", first.UUID, second.UUID)
	}
	if first.UUID == otherProtocol.UUID {
		t.Fatalf("placeholder uuid should include protocol: %s", first.UUID)
	}
	if first.UUID == otherSecret.UUID {
		t.Fatalf("placeholder uuid should include node secret: %s", first.UUID)
	}
	nw3 := placeholderServerUser(1, "nowhere", "secret")
	if key, err := node.NowhereSharedKey(nw3.UUID); err != nil || key != nw3.UUID {
		t.Fatalf("NW3 placeholder = %q, %v", nw3.UUID, err)
	}
}

func TestMergeSubscribeLists(t *testing.T) {
	sub1 := &subscribe.Subscribe{Id: 1}
	sub2 := &subscribe.Subscribe{Id: 2}
	sub3 := &subscribe.Subscribe{Id: 3}

	got := mergeSubscribeLists(
		[]*subscribe.Subscribe{sub1, sub2, nil},
		[]*subscribe.Subscribe{sub2, sub3},
		[]*subscribe.Subscribe{sub1},
	)

	if len(got) != 3 {
		t.Fatalf("mergeSubscribeLists() len = %d, want 3", len(got))
	}
	for i, wantID := range []int64{1, 2, 3} {
		if got[i].Id != wantID {
			t.Fatalf("mergeSubscribeLists()[%d].Id = %d, want %d", i, got[i].Id, wantID)
		}
	}
}
