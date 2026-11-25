package serverv1

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestNodeV1TrafficReportWireCompatibility(t *testing.T) {
	// V1 nodes send one nested record per subscription and use 64-bit byte counters.
	wire, err := hex.DecodeString("0a0a082a10808080801018030a04082b1807")
	require.NoError(t, err)
	var report PushUserTrafficRequest
	require.NoError(t, proto.Unmarshal(wire, &report))
	require.Len(t, report.Traffic, 2)
	first, second := report.Traffic[0], report.Traffic[1]
	require.EqualValues(t, 42, first.GetUserId())
	require.Equal(t, int64(1)<<32, first.GetUpload())
	require.EqualValues(t, 3, first.GetDownload())
	require.EqualValues(t, 43, second.GetUserId())
	require.Zero(t, second.GetUpload())
	require.EqualValues(t, 7, second.GetDownload())
}
