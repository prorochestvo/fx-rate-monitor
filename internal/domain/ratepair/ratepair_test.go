package ratepair_test

import (
	"sort"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/domain/ratepair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategoryOf(t *testing.T) {
	t.Parallel()

	t.Run("fiat base returns CategoryFiat", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ratepair.CategoryFiat, ratepair.CategoryOf("USD"))
		assert.Equal(t, ratepair.CategoryFiat, ratepair.CategoryOf("EUR"))
		assert.Equal(t, ratepair.CategoryFiat, ratepair.CategoryOf("KZT"))
	})

	t.Run("metal base returns CategoryMetal", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ratepair.CategoryMetal, ratepair.CategoryOf("GOLD"))
		assert.Equal(t, ratepair.CategoryMetal, ratepair.CategoryOf("SILVER"))
		assert.Equal(t, ratepair.CategoryMetal, ratepair.CategoryOf("PLATINUM"))
		assert.Equal(t, ratepair.CategoryMetal, ratepair.CategoryOf("PALLADIUM"))
	})

	t.Run("mixed case input is normalised", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ratepair.CategoryMetal, ratepair.CategoryOf("gold"))
		assert.Equal(t, ratepair.CategoryMetal, ratepair.CategoryOf("Gold"))
		assert.Equal(t, ratepair.CategoryMetal, ratepair.CategoryOf("gOlD"))
		assert.Equal(t, ratepair.CategoryFiat, ratepair.CategoryOf("usd"))
	})

	t.Run("empty string returns CategoryFiat", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ratepair.CategoryFiat, ratepair.CategoryOf(""))
	})

	t.Run("unknown long string returns CategoryFiat", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ratepair.CategoryFiat, ratepair.CategoryOf("RANDOMCURRENCY"))
		assert.Equal(t, ratepair.CategoryFiat, ratepair.CategoryOf("GOLDISH")) // has GOLD prefix but not exact
	})
}

func TestColorForPair(t *testing.T) {
	t.Parallel()

	t.Run("same input produces same output on multiple calls", func(t *testing.T) {
		t.Parallel()
		c1 := ratepair.ColorForPair("USD")
		c2 := ratepair.ColorForPair("USD")
		c3 := ratepair.ColorForPair("USD")
		assert.Equal(t, c1, c2)
		assert.Equal(t, c2, c3)
	})

	t.Run("fiat base produces a color from FiatPalette", func(t *testing.T) {
		t.Parallel()
		color := ratepair.ColorForPair("USD")
		assert.Contains(t, ratepair.FiatPalette, color)
	})

	t.Run("metal base produces a color from MetalPalette", func(t *testing.T) {
		t.Parallel()
		color := ratepair.ColorForPair("GOLD")
		assert.Contains(t, ratepair.MetalPalette, color)
	})

	t.Run("case-insensitive input produces the same color", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ratepair.ColorForPair("USD"), ratepair.ColorForPair("usd"))
		assert.Equal(t, ratepair.ColorForPair("USD"), ratepair.ColorForPair("Usd"))
		assert.Equal(t, ratepair.ColorForPair("GOLD"), ratepair.ColorForPair("gold"))
	})

	t.Run("different bases may produce different colors", func(t *testing.T) {
		t.Parallel()
		// USD and EUR are likely different colors; at least one pair should differ.
		// We cannot guarantee this absolutely but the FNV hash over a 6-element
		// palette makes collisions unlikely for typical FX bases.
		colors := make(map[string]bool)
		for _, base := range []string{"USD", "EUR", "GBP", "JPY", "RUB"} {
			colors[ratepair.ColorForPair(base)] = true
		}
		// At least 2 distinct colors across 5 bases (pigeonhole would give 1 at minimum).
		assert.Greater(t, len(colors), 1)
	})

	t.Run("returned color is a valid hex string from the relevant palette", func(t *testing.T) {
		t.Parallel()
		for _, base := range []string{"USD", "EUR", "GOLD", "SILVER", "KZT"} {
			color := ratepair.ColorForPair(base)
			require.True(t, len(color) > 0, "color for %s must not be empty", base)
			require.Equal(t, '#', rune(color[0]), "color for %s must start with #", base)
		}
	})

	t.Run("pinned palette index: hash function change must break this test", func(t *testing.T) {
		t.Parallel()
		// FNV-32a("USD") % 6 == 5; FNV-32a("GOLD") % 4 == 3.
		// If the hash function or palette layout ever changes, these assertions
		// will fail loudly and force an intentional update.
		assert.Equal(t, ratepair.FiatPalette[5], ratepair.ColorForPair("USD"))
		assert.Equal(t, ratepair.MetalPalette[3], ratepair.ColorForPair("GOLD"))
	})
}

func TestDedupe(t *testing.T) {
	t.Parallel()

	t.Run("empty input returns empty slice", func(t *testing.T) {
		t.Parallel()
		result := ratepair.Dedupe(nil)
		assert.Empty(t, result)
	})

	t.Run("single pair input is unchanged", func(t *testing.T) {
		t.Parallel()
		in := []ratepair.Pair{{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID}}
		result := ratepair.Dedupe(in)
		require.Len(t, result, 1)
		assert.Equal(t, in[0], result[0])
	})

	t.Run("multiple identical entries collapse to first occurrence", func(t *testing.T) {
		t.Parallel()
		in := []ratepair.Pair{
			{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID},
			{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID},
			{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID},
		}
		result := ratepair.Dedupe(in)
		require.Len(t, result, 1)
		assert.Equal(t, in[0], result[0])
	})

	t.Run("distinct pairs are all preserved", func(t *testing.T) {
		t.Parallel()
		in := []ratepair.Pair{
			{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID},
			{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindASK},
			{Base: "EUR", Quote: "KZT", Kind: domain.RateSourceKindBID},
		}
		result := ratepair.Dedupe(in)
		assert.Len(t, result, 3)
	})

	t.Run("input order is preserved in output", func(t *testing.T) {
		t.Parallel()
		in := []ratepair.Pair{
			{Base: "EUR", Quote: "KZT", Kind: domain.RateSourceKindBID},
			{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID},
			{Base: "EUR", Quote: "KZT", Kind: domain.RateSourceKindBID}, // duplicate
		}
		result := ratepair.Dedupe(in)
		require.Len(t, result, 2)
		assert.Equal(t, "EUR", result[0].Base)
		assert.Equal(t, "USD", result[1].Base)
	})

	t.Run("deduplication is case-insensitive on base and quote", func(t *testing.T) {
		t.Parallel()
		in := []ratepair.Pair{
			{Base: "usd", Quote: "kzt", Kind: domain.RateSourceKindBID},
			{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID},
		}
		result := ratepair.Dedupe(in)
		require.Len(t, result, 1)
		// First occurrence wins.
		assert.Equal(t, "usd", result[0].Base)
	})
}

func TestColorBidAsk(t *testing.T) {
	t.Parallel()

	t.Run("ColorBid is pinned to the agreed teal hex", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "#1D9E75", ratepair.ColorBid,
			"ColorBid must not change silently; update this test intentionally if the design changes")
	})

	t.Run("ColorAsk is pinned to the agreed blue hex", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "#378ADD", ratepair.ColorAsk,
			"ColorAsk must not change silently; update this test intentionally if the design changes")
	})

	t.Run("ColorBid and ColorAsk are distinct", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t, ratepair.ColorBid, ratepair.ColorAsk)
	})
}

func TestLess(t *testing.T) {
	t.Parallel()

	t.Run("fiat sorts before metal", func(t *testing.T) {
		t.Parallel()
		fiat := ratepair.Pair{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID}
		metal := ratepair.Pair{Base: "GOLD", Quote: "KZT", Kind: domain.RateSourceKindBID}
		assert.True(t, ratepair.Less(fiat, metal), "fiat must sort before metal")
		assert.False(t, ratepair.Less(metal, fiat), "metal must not sort before fiat")
	})

	t.Run("canonical pair ordering keeps BID and ASK rows adjacent", func(t *testing.T) {
		t.Parallel()
		// USD/KZT BID and KZT/USD ASK share canonical key USD/KZT.
		// Both must sort next to each other regardless of base direction.
		usdkztBID := ratepair.Pair{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID}
		kztusdASK := ratepair.Pair{Base: "KZT", Quote: "USD", Kind: domain.RateSourceKindASK}
		eurKZT := ratepair.Pair{Base: "EUR", Quote: "KZT", Kind: domain.RateSourceKindBID}

		pairs := []ratepair.Pair{eurKZT, kztusdASK, usdkztBID}
		sort.Slice(pairs, func(i, j int) bool { return ratepair.Less(pairs[i], pairs[j]) })

		// USD/KZT canonical sorts before EUR/KZT canonical ("EUR" < "USD" but
		// canonical EUR/KZT is "EUR/KZT" and canonical USD/KZT is "KZT/USD" → "EUR/KZT" < "KZT/USD").
		// After sort the two USD/KZT pairs must be adjacent.
		foundBID, foundASK := -1, -1
		for i, p := range pairs {
			if p.Base == "USD" && p.Quote == "KZT" {
				foundBID = i
			}
			if p.Base == "KZT" && p.Quote == "USD" {
				foundASK = i
			}
		}
		require.NotEqual(t, -1, foundBID, "BID row must appear in sorted output")
		require.NotEqual(t, -1, foundASK, "ASK row must appear in sorted output")
		assert.Equal(t, 1, foundASK-foundBID, "BID and ASK rows for the same canonical pair must be adjacent")
	})

	t.Run("BID sorts before ASK within the same canonical pair", func(t *testing.T) {
		t.Parallel()
		bid := ratepair.Pair{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID}
		ask := ratepair.Pair{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindASK}
		assert.True(t, ratepair.Less(bid, ask), "BID must sort before ASK")
		assert.False(t, ratepair.Less(ask, bid), "ASK must not sort before BID")
	})

	t.Run("equal pairs compare as not-less on both sides", func(t *testing.T) {
		t.Parallel()
		a := ratepair.Pair{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID}
		b := ratepair.Pair{Base: "USD", Quote: "KZT", Kind: domain.RateSourceKindBID}
		assert.False(t, ratepair.Less(a, b))
		assert.False(t, ratepair.Less(b, a))
	})
}
