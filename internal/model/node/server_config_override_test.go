package node

import "testing"

func TestServerConfigOverridePartialSavePreservesExistingFields(t *testing.T) {
	ctx := t.Context()
	client := deletionClient(t)
	model := NewModel(client, nil)
	server := client.Server.Create().SetName("edge").SaveX(ctx)
	if got, err := model.FindServerConfigOverride(ctx, server.ID); err != nil || got != nil {
		t.Fatalf("missing override = %+v, %v; want nil", got, err)
	}

	strategy, originalDNS := "prefer_ipv4", `{"servers":["192.0.2.53"]}`
	original := &ServerConfigOverride{ServerId: server.ID, IPStrategy: &strategy, DNS: &originalDNS}
	if err := model.SaveServerConfigOverride(ctx, original); err != nil {
		t.Fatal(err)
	}
	created, err := model.FindServerConfigOverride(ctx, server.ID)
	if err != nil || created == nil || created.IPStrategy == nil || *created.IPStrategy != strategy || created.DNS == nil || *created.DNS != originalDNS {
		t.Fatalf("created override = %+v, %v; want strategy and DNS", created, err)
	}

	newDNS := `{"servers":["192.0.2.54"]}`
	if err := model.SaveServerConfigOverride(ctx, &ServerConfigOverride{ServerId: server.ID, DNS: &newDNS}); err != nil {
		t.Fatal(err)
	}
	stored := client.ServerConfigOverride.Query().OnlyX(ctx)
	if stored.ID != created.Id || stored.ServerID != server.ID || !stored.CreatedAt.Equal(created.CreatedAt) ||
		stored.IPStrategy == nil || *stored.IPStrategy != strategy || stored.DNS == nil || *stored.DNS != newDNS {
		t.Fatalf("partial save persisted %+v; want original override with updated DNS and preserved strategy", stored)
	}
}
