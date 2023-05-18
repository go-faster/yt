package rpcclient

import (
	"context"
	"fmt"

	"github.com/go-faster/yt/bus"
	"github.com/golang/protobuf/proto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type TracingInterceptor struct {
	trace.Tracer
}

func (t *TracingInterceptor) traceStart(ctx context.Context, call *Call) (span trace.Span, retCtx context.Context) {
	log := call.Req.Log()

	attrs := make([]attribute.KeyValue, 0, len(log)+1)
	attrs = append(attrs, attribute.Stringer("call_id", call.CallID))
	for _, field := range log {
		if value, ok := field.Any().(fmt.Stringer); ok {
			attrs = append(attrs, attribute.Stringer(field.Key(), value))
		}
	}

	retCtx, span = t.Tracer.Start(ctx, string(call.Method),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return
}

func (t *TracingInterceptor) traceFinish(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
	}
	span.End()
}

func (t *TracingInterceptor) Intercept(ctx context.Context, call *Call, invoke CallInvoker, rsp proto.Message, opts ...bus.SendOption) (err error) {
	span, ctx := t.traceStart(ctx, call)
	err = invoke(ctx, call, rsp, opts...)
	t.traceFinish(span, err)
	return
}
