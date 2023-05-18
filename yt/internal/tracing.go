package internal

import (
	"context"
	"errors"
	"fmt"
	"io"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/atomic"
)

type tracingReader struct {
	span      trace.Span
	t         *TracingInterceptor
	r         io.ReadCloser
	call      *Call
	byteCount int64

	logged atomic.Bool
}

func (r *tracingReader) traceFinish(err error) {
	if !r.logged.Swap(true) {
		r.t.traceFinish(r.span, err, attribute.Int64("bytes_read", r.byteCount))
	}
}

func (r *tracingReader) Close() error {
	r.traceFinish(errors.New("request interrupted"))

	return r.r.Close()
}

func (r *tracingReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	r.byteCount += int64(n)

	if err != nil {
		if err == io.EOF {
			r.traceFinish(nil)
		} else {
			r.traceFinish(err)
		}
	}

	return
}

type tracingWriter struct {
	span      trace.Span
	t         *TracingInterceptor
	w         io.WriteCloser
	call      *Call
	byteCount int64

	logged atomic.Bool
}

func (w *tracingWriter) traceFinish(err error) {
	if !w.logged.Swap(true) {
		w.t.traceFinish(w.span, err, attribute.Int64("bytes_written", w.byteCount))
	}
}

func (w *tracingWriter) Write(p []byte) (n int, err error) {
	n, err = w.w.Write(p)
	w.byteCount += int64(n)

	if err != nil {
		w.traceFinish(err)
	}

	return
}

func (w *tracingWriter) Close() error {
	err := w.w.Close()
	w.traceFinish(err)
	return err
}

type TracingInterceptor struct {
	trace.Tracer
}

func (t *TracingInterceptor) traceStart(ctx context.Context, call *Call) (span trace.Span, retCtx context.Context) {
	log := call.Params.Log()

	attrs := make([]attribute.KeyValue, 0, len(log)+1)
	attrs = append(attrs, attribute.Stringer("call_id", call.CallID))
	for _, field := range log {
		if value, ok := field.Any().(fmt.Stringer); ok {
			attrs = append(attrs, attribute.Stringer(field.Key(), value))
		}
	}

	retCtx, span = t.Tracer.Start(ctx, string(call.Params.HTTPVerb()),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return
}

func (t *TracingInterceptor) traceFinish(span trace.Span, err error, attrs ...attribute.KeyValue) {
	// TODO(tdakkota): handle attrs?
	if err != nil {
		span.RecordError(err)
	}
	span.End()
}

func (t *TracingInterceptor) Intercept(ctx context.Context, call *Call, invoke CallInvoker) (res *CallResult, err error) {
	span, ctx := t.traceStart(ctx, call)
	res, err = invoke(ctx, call)
	t.traceFinish(span, err)
	return
}

func (t *TracingInterceptor) Read(ctx context.Context, call *Call, invoke ReadInvoker) (r io.ReadCloser, err error) {
	span, ctx := t.traceStart(ctx, call)
	r, err = invoke(ctx, call)
	if err != nil {
		return
	}

	r = &tracingReader{span: span, t: t, r: r, call: call}
	return
}

func (t *TracingInterceptor) Write(ctx context.Context, call *Call, invoke WriteInvoker) (w io.WriteCloser, err error) {
	span, ctx := t.traceStart(ctx, call)
	w, err = invoke(ctx, call)
	if err != nil {
		return
	}

	lw := &tracingWriter{span: span, t: t, w: w, call: call}
	if call.RowBatch != nil {
		lw.byteCount = int64(call.RowBatch.Len())
	}

	w = lw
	return
}
