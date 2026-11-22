package nekoproxy

import (
	"context"

	"github.com/perfect-panel/server/ent"
	"github.com/perfect-panel/server/internal/model/node"
	"github.com/perfect-panel/server/internal/repository"
)

func selectedPort(ctx context.Context, store repository.Store, n *node.Node, enabledOnly bool) (uint16, error) {
	if n.NodeType == "front" || n.ServerId == 0 || n.Protocol == "" || n.ProtocolId == "" {
		return 0, nil
	}
	s, err := store.Node().FindOneServer(ctx, n.ServerId)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	protocols, err := s.UnmarshalProtocols()
	if err != nil {
		return 0, err
	}
	for _, p := range protocols {
		if p.Type == n.Protocol && p.Id == n.ProtocolId && (!enabledOnly || p.Enable) {
			return p.Port, nil
		}
	}
	return 0, nil
}

// A nil marker is an older or newly attached forward: adopt it only if it still
// matches the previous protocol instance. Zero permanently pins a manual port.
func managedPortOnSave(ctx context.Context, store repository.Store, old, next *node.Node, marker *uint16, remotePort int) (uint16, error) {
	if marker != nil && *marker == 0 {
		return 0, nil
	}
	var expected uint16
	if marker == nil {
		port, err := selectedPort(ctx, store, old, false)
		if err != nil {
			return 0, err
		}
		expected = port
	} else {
		expected = *marker
	}
	if expected == 0 || remotePort != int(expected) {
		return 0, nil
	}
	if next.NodeType == "front" {
		return 0, nil
	}
	port, err := selectedPort(ctx, store, next, true)
	if err == nil && port == 0 {
		return 0, inputError("the selected server protocol has no active target port")
	}
	return port, err
}
