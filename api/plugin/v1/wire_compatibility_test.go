package pluginv1

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestGuestV1CanAbortWithHeadersAndBinaryBody(t *testing.T) {
	// This fixture uses the v1 guest wire layout independently of the current Go descriptor.
	wire, err := hex.DecodeString("0a0561626f727410f7031a0300ff0122100a0b52657472792d4166746572120135")
	require.NoError(t, err)
	var response MiddlewareResponse
	require.NoError(t, proto.Unmarshal(wire, &response))
	require.Equal(t, "abort", response.GetAction())
	require.EqualValues(t, 503, response.GetStatus())
	require.Equal(t, []byte{0, 255, 1}, response.GetBody())
	require.Equal(t, "5", response.GetHeaders()["Retry-After"])
}
