package yt

import (
	"github.com/go-faster/yt/guid"
	"github.com/go-faster/yt/ypath"
)

type (
	NodeAddress struct {
		NodeID    uint              `yson:"node_id"`
		Addresses map[string]string `yson:"addresses"`
	}

	ChunkSpec struct {
		ChunkID    guid.GUID        `yson:"chunk_id"`
		RangeIndex int              `yson:"range_index"`
		RowIndex   int              `yson:"row_index"`
		RowCount   int              `yson:"row_count"`
		LowerLimit *ypath.ReadLimit `yson:"lower_limit"`
		UpperLimit *ypath.ReadLimit `yson:"upper_limit"`
		Replicas   []int            `yson:"replicas"`
	}

	ShareLocation struct {
		Nodes      []NodeAddress `yson:"nodes"`
		ChunkSpecs []ChunkSpec   `yson:"chunk_specs"`
	}
)
