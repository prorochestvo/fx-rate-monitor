package ruledoctor_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seilbekskindirov/monitor/internal/ruledoctor"
)

func TestSnipForPair(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when pair not found", func(t *testing.T) {
		t.Parallel()
		got := ruledoctor.SnipForPair("<html>nothing here</html>", "MDL / KZT")
		assert.Empty(t, got)
	})

	t.Run("captures the rate row", func(t *testing.T) {
		t.Parallel()
		html := `<table><tr><td>1 ABC</td><td>ABC / KZT</td><td>1.23</td></tr><tr><td>1 МОЛДАВСКИЙ ЛЕЙ</td><td>MDL / KZT</td><td>27.07</td></tr><tr><td>1 XYZ</td><td>XYZ / KZT</td><td>9.99</td></tr></table>`
		got := ruledoctor.SnipForPair(html, "MDL / KZT")
		require.NotEmpty(t, got)
		assert.Contains(t, got, "MDL / KZT")
		assert.Contains(t, got, "27.07")
	})

	t.Run("handles pair at start of document", func(t *testing.T) {
		t.Parallel()
		html := `<tr><td>USD / KZT</td><td>462.91</td></tr>`
		got := ruledoctor.SnipForPair(html, "USD / KZT")
		require.NotEmpty(t, got)
		assert.Contains(t, got, "462.91")
	})
}

func TestClean(t *testing.T) {
	t.Parallel()

	t.Run("removes script and style blocks", func(t *testing.T) {
		t.Parallel()
		in := `<html><head><style>body{color:red}</style></head><body><script>alert(1)</script><div>keep</div></body></html>`
		out := ruledoctor.Clean(in)
		assert.NotContains(t, out, "alert")
		assert.NotContains(t, out, "color:red")
		assert.Contains(t, out, "keep")
	})

	t.Run("preserves currency tokens", func(t *testing.T) {
		t.Parallel()
		in := `<html><head><script>x=1</script></head><body><tr><td>MDL / KZT</td><td>27.07</td></tr></body></html>`
		out := ruledoctor.Clean(in)
		assert.Contains(t, out, "MDL / KZT")
		assert.Contains(t, out, "27.07")
	})

	t.Run("strips html comments", func(t *testing.T) {
		t.Parallel()
		in := `<div>before<!-- secret --> after</div>`
		out := ruledoctor.Clean(in)
		assert.NotContains(t, out, "secret")
		assert.Contains(t, out, "before")
		assert.Contains(t, out, "after")
	})

	t.Run("shrinks the canonical fixture meaningfully", func(t *testing.T) {
		t.Parallel()
		// synthetic: a chunk of script noise around a real-looking row
		in := strings.Repeat(`<script>var x=1;</script>`, 200) +
			`<tr><td>USD / KZT</td><td>462.91</td></tr>` +
			strings.Repeat(`<style>.a{}</style>`, 200)
		out := ruledoctor.Clean(in)
		assert.Less(t, len(out), len(in)/4, "cleaner should shrink heavy script/style noise")
		assert.Contains(t, out, "USD / KZT")
	})
}
