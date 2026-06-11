package sourceaudit

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPFetcher(t *testing.T) {
	t.Parallel()

	t.Run("empty proxyURL produces transport with nil Proxy field", func(t *testing.T) {
		t.Parallel()
		f, err := NewHTTPFetcher(30*time.Second, "")
		require.NoError(t, err)
		hf, ok := f.(*httpFetcher)
		require.True(t, ok)
		transport, ok := hf.client.Transport.(*http.Transport)
		require.True(t, ok, "transport must be *http.Transport, not nil or another type")
		assert.Nil(t, transport.Proxy,
			"Proxy func must be nil when proxyURL is empty — a nil Transport would trigger ProxyFromEnvironment")
	})

	t.Run("non-empty proxyURL produces transport with non-nil Proxy field", func(t *testing.T) {
		t.Parallel()
		f, err := NewHTTPFetcher(30*time.Second, "http://127.0.0.1:7788")
		require.NoError(t, err)
		hf, ok := f.(*httpFetcher)
		require.True(t, ok)
		transport, ok := hf.client.Transport.(*http.Transport)
		require.True(t, ok, "transport must be *http.Transport")
		assert.NotNil(t, transport.Proxy, "Proxy func must be set when proxyURL is non-empty")
	})

	t.Run("invalid proxyURL returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewHTTPFetcher(30*time.Second, "://bad-url")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse proxy URL")
	})
}
