package rateextractor

import (
	"net/http"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/tools/threadsafe"
	"github.com/stretchr/testify/require"
)

func TestParseJSONPath(t *testing.T) {
	t.Parallel()

	t.Run("simple key", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("rate")
		require.NoError(t, err)
		require.Equal(t, []pathSegment{{Key: "rate", HasIndex: false, Index: 0}}, segs)
	})
	t.Run("nested keys", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("usd.rate_value")
		require.NoError(t, err)
		require.Equal(t, []pathSegment{
			{Key: "usd", HasIndex: false, Index: 0},
			{Key: "rate_value", HasIndex: false, Index: 0},
		}, segs)
	})
	t.Run("array index", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("records[0].value")
		require.NoError(t, err)
		require.Equal(t, []pathSegment{
			{Key: "records", HasIndex: true, Index: 0},
			{Key: "value", HasIndex: false, Index: 0},
		}, segs)
	})
	t.Run("deep path with multiple indexes", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("a[1].b[2].c")
		require.NoError(t, err)
		require.Len(t, segs, 3)
		require.Equal(t, pathSegment{Key: "a", HasIndex: true, Index: 1}, segs[0])
		require.Equal(t, pathSegment{Key: "b", HasIndex: true, Index: 2}, segs[1])
		require.Equal(t, pathSegment{Key: "c", HasIndex: false, Index: 0}, segs[2])
	})
	t.Run("empty pattern", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("")
		require.Error(t, err)
		require.Nil(t, segs)
	})
	t.Run("empty segment", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("foo..bar")
		require.Error(t, err)
		require.Nil(t, segs)
	})
	t.Run("non-integer index", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("foo[abc]")
		require.Error(t, err)
		require.Nil(t, segs)
	})
	t.Run("negative-like index", func(t *testing.T) {
		t.Parallel()

		segs, err := parseJSONPath("foo[-1]")
		require.Error(t, err)
		require.Nil(t, segs)
	})
}

func TestRateExtractor_extractJSONPath(t *testing.T) {
	t.Parallel()

	newExtractor := func(t *testing.T) *RateExtractor {
		t.Helper()

		ext, err := NewRateExtractorWithHTTPClient(
			&mockRateValueRepository{},
			&http.Client{},
			threadsafe.NewBuffer(nil),
		)
		require.NoError(t, err)
		return ext
	}

	t.Run("simple numeric field", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("rate", []byte(`{"rate":450.75}`))
		require.NoError(t, err)
		require.Equal(t, []byte("450.75"), result)
	})
	t.Run("nested field", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("usd.rate_value", []byte(`{"usd":{"rate_value":99.9}}`))
		require.NoError(t, err)
		require.Equal(t, []byte("99.9"), result)
	})
	t.Run("array index", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("records[0].v", []byte(`{"records":[{"v":1.5}]}`))
		require.NoError(t, err)
		require.Equal(t, []byte("1.5"), result)
	})
	t.Run("string terminal value", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("rate", []byte(`{"rate":"123.0"}`))
		require.NoError(t, err)
		require.Equal(t, []byte("123.0"), result)
	})
	t.Run("integer json number", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("rate", []byte(`{"rate":500}`))
		require.NoError(t, err)
		require.Equal(t, []byte("500"), result)
	})
	t.Run("key not found", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("rate", []byte(`{"other":1}`))
		require.Error(t, err)
		require.Nil(t, result)
	})
	t.Run("index out of range", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("arr[5]", []byte(`{"arr":[1,2]}`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "out of range")
		require.Nil(t, result)
	})
	t.Run("not an array", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("arr[0]", []byte(`{"arr":{"x":1}}`))
		require.Error(t, err)
		require.Nil(t, result)
	})
	t.Run("not an object", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("rate", []byte(`[1,2,3]`))
		require.Error(t, err)
		require.Nil(t, result)
	})
	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("rate", []byte(`not-json`))
		require.Error(t, err)
		require.Nil(t, result)
	})
	t.Run("terminal is object", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("a", []byte(`{"a":{}}`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported type")
		require.Nil(t, result)
	})
	t.Run("terminal is array", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("a", []byte(`{"a":[]}`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported type")
		require.Nil(t, result)
	})
	t.Run("invalid path", func(t *testing.T) {
		t.Parallel()

		ext := newExtractor(t)
		result, err := ext.extractJSONPath("", []byte(`{"rate":1}`))
		require.Error(t, err)
		require.Nil(t, result)
	})
}
