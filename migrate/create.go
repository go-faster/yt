package migrate

import (
	"context"

	"github.com/go-faster/yt/schema"
	"github.com/go-faster/yt/ypath"
	"github.com/go-faster/yt/yt"
)

// Create creates new dynamic table with provided schema.
func Create(ctx context.Context, yc yt.Client, path ypath.Path, schema schema.Schema) error {
	_, err := yc.CreateNode(ctx, path, yt.NodeTable, &yt.CreateNodeOptions{
		Recursive: true,
		Attributes: map[string]interface{}{
			"dynamic": true,
			"schema":  schema,
		},
	})

	return err
}
