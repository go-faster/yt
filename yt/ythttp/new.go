// package ythttp provides YT client over HTTP protocol.
package ythttp

import (
	"testing"

	"github.com/go-faster/errors"

	"github.com/go-faster/yt/mapreduce"
	"github.com/go-faster/yt/yt"
	"github.com/go-faster/yt/yt/internal/httpclient"
)

func checkNotInsideJob(c *yt.Config) error {
	if c.AllowRequestsFromJob {
		return nil
	}

	if mapreduce.InsideJob() {
		return errors.New("requests to cluster from inside job are forbidden")
	}

	return nil
}

// NewClient creates new client from config.
func NewClient(c *yt.Config) (yt.Client, error) {
	if err := checkNotInsideJob(c); err != nil {
		return nil, err
	}

	return httpclient.NewHTTPClient(c)
}

// NewTestClient creates new client from config to be used in integration tests.
func NewTestClient(t testing.TB, c *yt.Config) (yt.Client, error) {
	if err := checkNotInsideJob(c); err != nil {
		return nil, err
	}

	return httpclient.NewTestHTTPClient(t, c)
}
