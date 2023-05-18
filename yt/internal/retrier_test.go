package internal

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-faster/errors"
	"github.com/stretchr/testify/assert"

	"github.com/go-faster/yt/ypath"
	"github.com/go-faster/yt/yt"
)

type netError struct {
	timeout   bool
	temporary bool
}

func (n *netError) Error() string {
	return fmt.Sprintf("%#v", n)
}

func (n *netError) Timeout() bool {
	return n.timeout
}

func (n *netError) Temporary() bool {
	return n.temporary
}

var _ net.Error = &netError{}

func TestReadOnlyMethods(t *testing.T) {
	for _, p := range []interface{}{
		&GetNodeParams{},
		&ListNodeParams{},
		&NodeExistsParams{},

		&GetOperationParams{},
		&ListOperationsParams{},

		&GetFileFromCacheParams{},
	} {
		_, ok := p.(ReadRetryParams)
		assert.True(t, ok, "%T does not implement ReadRetryParams", p)
	}
}

func TestReadRetrierRetriesGet(t *testing.T) {
	r := &Retrier{Config: &yt.Config{}}

	call := &Call{
		Params:  NewGetNodeParams(ypath.Root, nil),
		Backoff: &backoff.ZeroBackOff{},
	}

	var failed bool

	_, err := r.Intercept(context.Background(), call, func(context.Context, *Call) (*CallResult, error) {
		if !failed {
			failed = true
			return &CallResult{}, errors.Wrap(&netError{timeout: true}, "request failed")
		}

		return &CallResult{}, nil
	})

	assert.True(t, failed)
	assert.NoError(t, err)
}

func TestReadRetrierIgnoresMutations(t *testing.T) {
	r := &Retrier{Config: &yt.Config{}}

	call := &Call{
		Params:  NewSetNodeParams(ypath.Root, nil),
		Backoff: &backoff.ZeroBackOff{},
	}

	_, err := r.Intercept(context.Background(), call, func(context.Context, *Call) (*CallResult, error) {
		return &CallResult{}, errors.New("request failed")
	})

	assert.Error(t, err)
}
