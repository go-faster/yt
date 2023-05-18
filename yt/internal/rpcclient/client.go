package rpcclient

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-faster/yt/bus"
	"github.com/go-faster/yt/proto/client/api/rpc_proxy"
	"github.com/go-faster/yt/ypath"
	"github.com/go-faster/yt/yt"
	"github.com/go-faster/yt/yt/internal"
	"github.com/golang/protobuf/proto"
	"go.opentelemetry.io/otel/trace"
	"go.ytsaurus.tech/library/go/core/log"
	"go.ytsaurus.tech/library/go/core/log/ctxlog"
	"go.ytsaurus.tech/library/go/core/xerrors"
)

var _ yt.Client = (*client)(nil)

type client struct {
	Encoder

	conf           *yt.Config
	httpClusterURL yt.ClusterURL
	rpcClusterURL  yt.ClusterURL
	token          string

	log    log.Structured
	tracer trace.Tracer

	// httpClient is used to retrieve available proxies.
	httpClient *http.Client
	proxySet   *internal.ProxySet

	connPool *connPool
	stop     *internal.StopGroup
}

func NewClient(conf *yt.Config) (*client, error) {
	c := &client{
		conf:           conf,
		httpClusterURL: yt.NormalizeProxyURL(conf.Proxy, conf.UseTVMOnlyEndpoint, yt.TVMOnlyHTTPProxyPort),
		rpcClusterURL:  yt.NormalizeProxyURL(conf.RPCProxy, conf.UseTVMOnlyEndpoint, yt.TVMOnlyRPCProxyPort),
		log:            conf.GetLogger(),
		tracer:         conf.GetTracer("yt.rpcclient"),
		stop:           internal.NewStopGroup(),
	}

	certPool, err := internal.NewCertPool()
	if err != nil {
		return nil, err
	}

	c.httpClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        0,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     30 * time.Second,

			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		},
		Timeout: 60 * time.Second,
	}

	if token := conf.GetToken(); token != "" {
		c.token = token
	}

	c.proxySet = &internal.ProxySet{UpdateFn: c.listRPCProxies}

	c.connPool = NewConnPool(func(ctx context.Context, addr string) BusConn {
		clientOpts := []bus.ClientOption{
			bus.WithLogger(c.log.Logger()),
			bus.WithDefaultProtocolVersionMajor(ProtocolVersionMajor),
		}
		return bus.NewClient(ctx, addr, clientOpts...)
	}, c.log)

	c.Encoder.StartCall = c.startCall
	c.Encoder.Invoke = c.invoke
	c.Encoder.InvokeReadRow = c.doReadRow

	proxyBouncer := &ProxyBouncer{Log: c.log, ProxySet: c.proxySet, ConnPool: c.connPool}
	requestLogger := &LoggingInterceptor{Structured: c.log}
	requestTracer := &TracingInterceptor{Tracer: c.tracer}
	mutationRetrier := &MutationRetrier{Log: c.log}
	readRetrier := &Retrier{Config: c.conf, Log: c.log}
	errorWrapper := &ErrorWrapper{}

	c.Encoder.Invoke = c.Encoder.Invoke.
		Wrap(proxyBouncer.Intercept).
		Wrap(requestLogger.Intercept).
		Wrap(requestTracer.Intercept).
		Wrap(mutationRetrier.Intercept).
		Wrap(readRetrier.Intercept).
		Wrap(errorWrapper.Intercept)

	return c, nil
}

func (c *client) schema() string {
	schema := "http"
	if c.conf.UseTLS {
		schema = "https"
	}
	return schema
}

func (c *client) doReadRow(
	ctx context.Context,
	call *Call,
	rsp ProtoRowset,
) (yt.TableReader, error) {
	var rspAttachments [][]byte

	err := c.Invoke(ctx, call, rsp, bus.WithResponseAttachments(&rspAttachments))
	if err != nil {
		return nil, err
	}

	rows, err := decodeFromWire(rspAttachments)
	if err != nil {
		err := xerrors.Errorf("unable to decode response from wire format: %w", err)
		return nil, err
	}

	return newTableReader(rows, rsp.GetRowsetDescriptor())
}

func (c *client) invoke(
	ctx context.Context,
	call *Call,
	rsp proto.Message,
	opts ...bus.SendOption,
) error {
	addr := call.RequestedProxy
	if addr == "" {
		var err error
		addr, err = c.pickRPCProxy(ctx)
		if err != nil {
			return err
		}
	}
	call.SelectedProxy = addr

	opts = append(opts,
		bus.WithRequestID(call.CallID),
	)

	credentials, err := c.requestCredentials(ctx)
	if err != nil {
		return err
	}
	opts = append(opts, bus.WithCredentials(credentials))

	if call.Attachments != nil {
		opts = append(opts, bus.WithAttachments(call.Attachments...))
	}

	c.injectTracing(ctx, &opts)

	conn, err := c.getConn(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Release()

	ctxlog.Debug(ctx, c.log.Logger(), "sending RPC request",
		log.String("proxy", call.SelectedProxy),
		log.String("request_id", call.CallID.String()),
	)

	start := time.Now()
	err = conn.Send(ctx, "ApiService", string(call.Method), call.Req, rsp, opts...)
	duration := time.Since(start)

	ctxlog.Debug(ctx, c.log.Logger(), "received RPC response",
		log.String("proxy", call.SelectedProxy),
		log.String("request_id", call.CallID.String()),
		log.Bool("ok", err == nil),
		log.Duration("duration", duration))

	if errors.Is(err, bus.ErrConnClosed) {
		conn.Discard()
	}

	return err
}

func (c *client) requestCredentials(ctx context.Context) (yt.Credentials, error) {
	if creds := yt.ContextCredentials(ctx); creds != nil {
		return creds, nil
	}

	if c.conf.TVMFn != nil {
		ticket, err := c.conf.TVMFn(ctx)
		if err != nil {
			return nil, err
		}

		credentials := &yt.ServiceTicketCredentials{Ticket: ticket}
		return credentials, nil
	}

	credentials := &yt.TokenCredentials{Token: c.token}
	return credentials, nil
}

func (c *client) getConn(ctx context.Context, addr string) (*conn, error) {
	dial, ok := GetDialer(ctx)
	if ok {
		conn := dial(ctx, addr)
		wrapped := newConn(addr, conn, nil)
		return wrapped, nil
	}
	return c.connPool.Conn(ctx, addr)
}

func (c *client) Stop() {
	c.connPool.Stop()
	c.stop.Stop()
}

func (c *client) startCall() *Call {
	bf := backoff.NewExponentialBackOff()
	bf.MaxElapsedTime = c.conf.GetLightRequestTimeout()
	return &Call{
		Backoff: bf,
	}
}

func (c *client) injectTracing(ctx context.Context, opts *[]bus.SendOption) {
	if c.conf.TraceFn == nil {
		return
	}

	traceID, spanID, flags, ok := c.conf.TraceFn(ctx)
	if !ok {
		return
	}

	*opts = append(*opts, bus.WithTracing(traceID, spanID, flags))
}

// LockRows wraps encoder's implementation with transaction.
func (c *client) LockRows(
	ctx context.Context,
	path ypath.Path,
	locks []string,
	lockType yt.LockType,
	keys []interface{},
	opts *yt.LockRowsOptions,
) (err error) {
	if opts == nil {
		opts = &yt.LockRowsOptions{}
	}
	if opts.TransactionOptions == nil {
		opts.TransactionOptions = &yt.TransactionOptions{}
	}

	if len(keys) == 0 {
		return nil
	}

	var zero yt.TxID
	if opts.TransactionID != zero {
		return c.Encoder.LockRows(ctx, path, locks, lockType, keys, opts)
	}

	tx, err := c.BeginTabletTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Abort()

	opts.TransactionID = tx.ID()

	err = c.Encoder.LockRows(ctx, path, locks, lockType, keys, opts)
	if err != nil {
		return err
	}

	return tx.Commit()
}

type rowBatch struct {
	rows        []interface{}
	rowCount    int
	attachments [][]byte
	descriptor  *rpc_proxy.TRowsetDescriptor
}

func (b *rowBatch) Write(row interface{}) error {
	b.rows = append(b.rows, row)
	b.rowCount++
	return nil
}

func (b *rowBatch) Commit() error {
	if len(b.rows) == 0 {
		return nil
	}

	var err error
	b.attachments, b.descriptor, err = encodeToWire(b.rows)
	b.rows = nil
	return err
}

func (b *rowBatch) Rollback() error {
	return nil
}

func (b *rowBatch) Len() int {
	var l int
	for _, b := range b.attachments {
		l += len(b)
	}
	return l
}

func (b *rowBatch) Batch() yt.RowBatch {
	if b.rows != nil {
		panic("reading unfinished batch")
	}

	return b
}

func (c *client) NewRowBatchWriter() yt.RowBatchWriter {
	return &rowBatch{}
}

func buildBatch(rows []interface{}) (yt.RowBatch, error) {
	var b rowBatch
	if len(rows) == 0 {
		return &b, nil
	}

	var err error
	b.attachments, b.descriptor, err = encodeToWire(rows)
	b.rowCount = len(rows)
	return &b, err
}

// InsertRows wraps encoder's implementation with transaction.
func (c *client) InsertRows(
	ctx context.Context,
	path ypath.Path,
	rows []interface{},
	opts *yt.InsertRowsOptions,
) (err error) {
	if len(rows) == 0 {
		return nil
	}

	batch, err := buildBatch(rows)
	if err != nil {
		return err
	}

	return c.InsertRowBatch(ctx, path, batch, opts)
}

func (c *client) InsertRowBatch(
	ctx context.Context,
	path ypath.Path,
	rowBatch yt.RowBatch,
	opts *yt.InsertRowsOptions,
) (err error) {
	if opts == nil {
		opts = &yt.InsertRowsOptions{}
	}
	if opts.TransactionOptions == nil {
		opts.TransactionOptions = &yt.TransactionOptions{}
	}

	var zero yt.TxID
	if opts.TransactionID != zero {
		return c.Encoder.InsertRowBatch(ctx, path, rowBatch, opts)
	}

	tx, err := c.BeginTabletTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Abort()

	opts.TransactionID = tx.ID()

	err = tx.InsertRowBatch(ctx, path, rowBatch, opts)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteRows wraps encoder's implementation with transaction.
func (c *client) DeleteRows(
	ctx context.Context,
	path ypath.Path,
	keys []interface{},
	opts *yt.DeleteRowsOptions,
) (err error) {
	if opts == nil {
		opts = &yt.DeleteRowsOptions{}
	}
	if opts.TransactionOptions == nil {
		opts.TransactionOptions = &yt.TransactionOptions{}
	}

	if len(keys) == 0 {
		return nil
	}

	var zero yt.TxID
	if opts.TransactionID != zero {
		return c.Encoder.DeleteRows(ctx, path, keys, opts)
	}

	tx, err := c.BeginTabletTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Abort()

	opts.TransactionID = tx.ID()

	err = c.Encoder.DeleteRows(ctx, path, keys, opts)
	if err != nil {
		return err
	}

	return tx.Commit()
}
