package rateextractor

import (
	"context"
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

	t.Run("fetchRenderedPageInAllocator short-circuits after prior failure", func(t *testing.T) {
		t.Parallel()

		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})

		prior := errors.New("prior network error")
		e.recordFailedURL("https://example.com/rates", prior)

		src := &domain.RateSource{Name: "src", URL: "https://example.com/rates"}
		payload, err := e.fetchRenderedPageInAllocator(t.Context(), src)

		require.Nil(t, payload)
		require.Error(t, err)
		require.ErrorContains(t, err, "short-circuit")
		require.ErrorContains(t, err, "tombstoned this run")
		require.ErrorIs(t, err, prior)
	})
}

func TestChromedpRateExtractor_RunBatch(t *testing.T) {
	t.Parallel()

	t.Run("empty batch is a no-op and does not launch Chromium", func(t *testing.T) {
		t.Parallel()

		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})
		out := e.RunBatch(t.Context(), nil)
		require.Nil(t, out)
	})

	t.Run("cancelled parent ctx tags every source without launching Chromium", func(t *testing.T) {
		t.Parallel()

		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		batch := []*domain.RateSource{
			{Name: "src1", URL: "https://example.com/a"},
			{Name: "src2", URL: "https://example.com/b"},
		}
		out := e.RunBatch(ctx, batch)

		require.Len(t, out, 2)
		require.ErrorIs(t, out["src1"], context.Canceled)
		require.ErrorIs(t, out["src2"], context.Canceled)
		require.ErrorContains(t, out["src1"], "src1")
		require.ErrorContains(t, out["src2"], "src2")
	})

	t.Run("tombstoned URL in batch short-circuits to the cached error", func(t *testing.T) {
		t.Parallel()

		// Live ctx + single tombstoned source. newExecAllocator returns a
		// usable allocCtx without launching Chromium (chromedp.NewExecAllocator
		// is lazy — the subprocess spawns on first NewContext / Run), and the
		// tombstone short-circuit fires inside fetchRenderedPageInAllocator
		// before any chromedp.NewContext is invoked. The whole path stays
		// hermetic without a real Chromium binary.
		e := NewChromedpRateExtractor("", io.Discard, &mockRateValueRepository{})
		prior := errors.New("prior network error")
		e.recordFailedURL("https://example.com/dead", prior)

		batch := []*domain.RateSource{
			{Name: "dead", URL: "https://example.com/dead"},
		}
		out := e.RunBatch(t.Context(), batch)

		require.Len(t, out, 1)
		require.ErrorIs(t, out["dead"], prior, "tombstoned URL must surface the cached error")
		require.ErrorContains(t, out["dead"], "short-circuit")
		require.ErrorContains(t, out["dead"], "tombstoned this run")
	})
}
