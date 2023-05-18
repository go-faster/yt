package httpclient

import (
	"bytes"
	"fmt"

	"github.com/go-faster/yt/yt"
	"go.ytsaurus.tech/library/go/blockcodecs"
)

type rowBatch struct {
	buf bytes.Buffer
}

func (b *rowBatch) Len() int {
	return b.buf.Len()
}

func (b *rowBatch) Write(p []byte) (n int, err error) {
	return b.buf.Write(p)
}

func (b *rowBatch) Close() error {
	return nil
}

type errTableWriter struct {
	err error
}

func (ew errTableWriter) Write(row interface{}) error {
	return ew.err
}

func (ew errTableWriter) Commit() error {
	return ew.err
}

func (ew errTableWriter) Rollback() error {
	return ew.err
}

type rowBatchWriter struct {
	yt.TableWriter
	batch *rowBatch
}

func (bw *rowBatchWriter) Batch() yt.RowBatch {
	return bw.batch
}

func (c *httpClient) NewRowBatchWriter() yt.RowBatchWriter {
	batch := &rowBatch{}

	switch c.config.GetClientCompressionCodec() {
	case yt.ClientCodecGZIP, yt.ClientCodecNone:
		return &rowBatchWriter{newTableWriter(batch, nil), batch}
	default:
		block, ok := c.config.GetClientCompressionCodec().BlockCodec()
		if !ok {
			err := fmt.Errorf("unsupported compression codec %d", c.config.GetClientCompressionCodec())
			return &rowBatchWriter{errTableWriter{err}, batch}
		}

		codec := blockcodecs.FindCodecByName(block)
		if codec == nil {
			err := fmt.Errorf("unsupported compression codec %q", block)
			return &rowBatchWriter{errTableWriter{err}, batch}
		}

		encoder := blockcodecs.NewEncoder(&batch.buf, codec)
		return &rowBatchWriter{newTableWriter(encoder, nil), batch}
	}
}
