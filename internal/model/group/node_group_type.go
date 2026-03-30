package group

import (
	"errors"
	"strings"
)

const (
	NodeGroupTypeCommon    = "common"
	NodeGroupTypeSubscribe = "subscribe"
	NodeGroupTypeApp       = "app"

	NodeGroupAccessSubscribe = "subscribe"
	NodeGroupAccessApp       = "app"
)

var ErrInvalidNodeGroupType = errors.New("invalid node group type")

func NormalizeNodeGroupType(nodeGroupType string) string {
	return strings.ToLower(strings.TrimSpace(nodeGroupType))
}

func ResolveNodeGroupType(nodeGroupType string) (string, error) {
	switch NormalizeNodeGroupType(nodeGroupType) {
	case "", NodeGroupTypeCommon:
		return NodeGroupTypeCommon, nil
	case NodeGroupTypeSubscribe:
		return NodeGroupTypeSubscribe, nil
	case NodeGroupTypeApp:
		return NodeGroupTypeApp, nil
	default:
		return "", ErrInvalidNodeGroupType
	}
}

func MustNodeGroupType(nodeGroupType string) string {
	resolved, err := ResolveNodeGroupType(nodeGroupType)
	if err != nil {
		return NodeGroupTypeCommon
	}
	return resolved
}

func IsNodeGroupTypeAccessible(nodeGroupType, accessType string) bool {
	switch accessType {
	case NodeGroupAccessSubscribe:
		resolved := MustNodeGroupType(nodeGroupType)
		return resolved == NodeGroupTypeCommon || resolved == NodeGroupTypeSubscribe
	case NodeGroupAccessApp:
		resolved := MustNodeGroupType(nodeGroupType)
		return resolved == NodeGroupTypeCommon || resolved == NodeGroupTypeApp
	default:
		return false
	}
}
