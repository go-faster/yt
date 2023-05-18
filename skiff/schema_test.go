package skiff

import (
	"testing"

	"github.com/go-faster/yt/yson"
	"github.com/stretchr/testify/require"
)

func TestWireTypeYSON(t *testing.T) {
	for wire := TypeNothing; wire <= TypeTuple; wire++ {
		ys, err := yson.Marshal(wire)
		require.NoError(t, err)

		var decodedWire WireType
		err = yson.Unmarshal(ys, &decodedWire)
		require.NoError(t, err)

		require.Equal(t, wire, decodedWire)
	}
}
