package rateextractor

import (
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestChromedpRateExtractor_failFast(t *testing.T) {
	t.Parallel()

	t.Run("recordFailedURL then loadFailedURL returns true", func(t *testing.T) {
		t.Parallel()

		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})

		sentinel := errors.New("sentinel error")
		e.recordFailedURL("https://example.com/rates", sentinel)

		got, ok := e.loadFailedURL("https://example.com/rates")
		require.True(t, ok, "loadFailedURL must return true for a recorded URL")
		require.Equal(t, sentinel, got, "loadFailedURL must return the stored error")
	})

	t.Run("loadFailedURL on unrecorded URL returns false", func(t *testing.T) {
		t.Parallel()

		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})

		got, ok := e.loadFailedURL("https://not-recorded.example.com/")
		require.False(t, ok, "loadFailedURL must return false for an unrecorded URL")
		require.Nil(t, got)
	})

	t.Run("recordFailedURL is concurrency-safe", func(t *testing.T) {
		t.Parallel()

		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})

		const workers = 20
		const concurrentURL = "https://example.com/concurrent"
		sentinel := errors.New("concurrent sentinel")

		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				e.recordFailedURL(concurrentURL, sentinel)
				_, _ = e.loadFailedURL(concurrentURL)
			}()
		}
		wg.Wait()

		_, ok := e.loadFailedURL(concurrentURL)
		require.True(t, ok, "URL must be recorded after concurrent writes")
	})

	t.Run("fetchRenderedPage short-circuits after prior failure", func(t *testing.T) {
		t.Parallel()

		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})

		prior := errors.New("prior network error")
		e.recordFailedURL("https://example.com/rates", prior)

		src := &domain.RateSource{Name: "src", URL: "https://example.com/rates"}
		payload, err := e.fetchRenderedPage(t.Context(), src)

		require.Nil(t, payload)
		require.Error(t, err)
		require.ErrorContains(t, err, "short-circuit")
		require.ErrorContains(t, err, "tombstoned this run")
		require.ErrorIs(t, err, prior)
	})
}
