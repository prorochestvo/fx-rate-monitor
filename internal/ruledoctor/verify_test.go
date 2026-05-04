package ruledoctor_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/seilbekskindirov/monitor/internal/ruledoctor"
)

func TestVerify(t *testing.T) {
	t.Parallel()

	html := `<table><tr id="r1"><td class="pair">MDL / KZT</td><td class="rate">27.07</td></tr></table>`

	t.Run("all three matchers pass on a clean rule", func(t *testing.T) {
		t.Parallel()
		ex := &ruledoctor.Extraction{
			Value:       "27.07",
			CSSSelector: "tr#r1 td.rate",
			Regex:       `<td class="rate">(\d+\.\d+)</td>`,
		}
		r := ruledoctor.Verify(html, "27.07", ex)
		assert.True(t, r.ValueMatches)
		assert.True(t, r.CSSMatches)
		assert.True(t, r.RegexMatches)
		assert.NoError(t, r.CSSError)
		assert.NoError(t, r.RegexError)
	})

	t.Run("invalid regex is reported, not panicked", func(t *testing.T) {
		t.Parallel()
		ex := &ruledoctor.Extraction{
			Value:       "27.07",
			CSSSelector: "tr#r1 td.rate",
			Regex:       `(?<lookbehind>foo)`, // not RE2
		}
		r := ruledoctor.Verify(html, "27.07", ex)
		assert.True(t, r.ValueMatches)
		assert.True(t, r.CSSMatches)
		assert.False(t, r.RegexMatches)
		assert.Error(t, r.RegexError)
	})

	t.Run("css selector that matches nothing is reported", func(t *testing.T) {
		t.Parallel()
		ex := &ruledoctor.Extraction{
			Value:       "27.07",
			CSSSelector: "td.does-not-exist",
			Regex:       `<td class="rate">(\d+\.\d+)</td>`,
		}
		r := ruledoctor.Verify(html, "27.07", ex)
		assert.True(t, r.ValueMatches)
		assert.False(t, r.CSSMatches)
		assert.Error(t, r.CSSError)
		assert.True(t, r.RegexMatches)
	})

	t.Run("value mismatch is reported", func(t *testing.T) {
		t.Parallel()
		ex := &ruledoctor.Extraction{
			Value:       "999.99",
			CSSSelector: "tr#r1 td.rate",
			Regex:       `<td class="rate">(\d+\.\d+)</td>`,
		}
		r := ruledoctor.Verify(html, "27.07", ex)
		assert.False(t, r.ValueMatches)
		assert.True(t, r.CSSMatches)
		assert.True(t, r.RegexMatches)
	})
}
