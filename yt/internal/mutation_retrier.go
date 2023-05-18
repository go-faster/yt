package internal

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-faster/yt/guid"
	"github.com/go-faster/yt/yt"
	"go.ytsaurus.tech/library/go/core/log"
	"go.ytsaurus.tech/library/go/core/log/ctxlog"
)

type MutationRetrier struct {
	Log log.Structured
}

type MutatingParams interface {
	MutatingOptions() **yt.MutatingOptions
}

func (r *MutationRetrier) Intercept(ctx context.Context, call *Call, invoke CallInvoker) (res *CallResult, err error) {
	if params, ok := call.Params.(MutatingParams); ok && !call.DisableRetries {
		mut := params.MutatingOptions()
		*mut = &yt.MutatingOptions{MutationID: yt.MutationID(guid.New())}

		for i := 0; ; i++ {
			res, err = invoke(ctx, call)
			if err == nil || !isNetError(err) {
				return
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			(*mut).Retry = true

			b := call.Backoff.NextBackOff()
			if b == backoff.Stop {
				return
			}

			if r.Log != nil {
				ctxlog.Warn(ctx, r.Log.Logger(), "retrying mutation",
					log.String("call_id", call.CallID.String()),
					log.Duration("backoff", b),
					log.Error(err))
			}

			select {
			case <-time.After(b):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	} else {
		return invoke(ctx, call)
	}
}
