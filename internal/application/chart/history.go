package chart

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// HistoryValuesLoader loads paginated rate_values for a bulk set of
// (source, base, quote) keys, sorted newest first. The single method returns
// both the row-level and the grouped-total counts in one consistent snapshot,
// eliminating the possibility of a collector write making the two counts
// disagree (see plans/015-history-group-by-provider.md, Risk 2).
type HistoryValuesLoader interface {
	// ObtainHistoryForPairsPaged returns paginated rate_values rows, the
	// row-level total, and the grouped total of distinct (provider title,
	// timestamp) tuples. All three queries run inside a single read-only
	// transaction so callers get a consistent snapshot. Use groupedTotal as
	// the pagination total shown to users; rowTotal is for diagnostics.
	ObtainHistoryForPairsPaged(
		ctx context.Context, pairs []domain.SourcePairKey, limit, offset int64,
	) (rows []domain.RateValue, rowTotal int64, groupedTotal int64, err error)
}

// MeHistoryResult is the result of ObtainMeHistory: one page of rate-collection
// events for a canonical pair, sorted newest first.
type MeHistoryResult struct {
	// Pair is the canonical pair label the results are scoped to (e.g. "USD/KZT").
	Pair string
	// Total is the unpaginated count of distinct (title, timestamp) tuples.
	Total int64
	// Items contains the page rows, newest first.
	Items []MeHistoryRowResult
}

// MeHistoryRowResult is one grouped rate-collection event for a single
// (title, timestamp) tuple. BID and ASK from sibling sources sharing the
// same provider title at the same scrape moment are collapsed into one row.
type MeHistoryRowResult struct {
	// SourceTitle is the human-readable provider title that acts as the grouping key.
	SourceTitle string
	// Timestamp is when the collector scraped this value.
	Timestamp time.Time
	// Bid is the BID price; nil when the provider only scraped ASK at this moment.
	Bid *float64
	// Ask is the ASK price; nil when the provider only scraped BID at this moment.
	Ask *float64
	// BidDeltaPct is the percent change from the previous BID observation in
	// this (title, direction) chain within the page. Nil for the first row.
	BidDeltaPct *float64
	// AskDeltaPct is the percent change from the previous ASK observation in
	// this (title, direction) chain within the page. Nil for the first row.
	AskDeltaPct *float64
}

// ObtainMeHistory returns paginated rate-collection events for the calling
// user's subscribed sources that match the given canonical pair label (e.g.
// "USD/KZT"). Results are sorted newest first.
//
// page is 1-based. limit is bounded by the caller; the service does no
// re-bounding. When the user has no subscriptions matching pair, the result
// has zero items and Total=0 (not an error).
//
// sourceTitle is an optional filter. When non-empty, only rows from providers
// whose title matches sourceTitle exactly (byte-for-byte, case-sensitive) are
// included. The filter is applied at the service layer by pre-filtering the
// matchingKeys slice (which retains all source names for sibling sources of the
// same provider) before it is passed to the repository. Total reflects the
// filtered grouped count and pagination math stays correct. An unknown
// sourceTitle (one not present in the user's subscriptions for this pair)
// returns 200 with Total=0 and empty Items — the service never returns 400 for
// this case.
//
// Load-bearing invariants (see plans/015-history-group-by-provider.md):
//
//   - Invariant 1: Each provider has at most one BID source and one ASK source
//     per (base, quote) pair. If two BID sources share the same title for one pair,
//     only the last value seen in the page wins within the (title, timestamp) group.
//
//   - Invariant 2: SourceTitle is unique per provider across all sources a user
//     is subscribed to. Two unrelated providers sharing the same title string
//     would be merged into one chip and one row. The seeds enforce this today;
//     do not add a new provider with a title that duplicates an existing one.
//
// The returned MeHistoryResult always groups BID and ASK observations for the
// same (title, timestamp) into one MeHistoryRowResult. Delta percent is
// computed against the previous sample for the same (title, direction) chain
// in the current page — the first row in any chain has nil delta (v1 known
// limitation: cross-page anchoring is deferred).
func (s *Service) ObtainMeHistory(ctx context.Context, userID, pair, sourceTitle string, page, limit int64) (*MeHistoryResult, error) {
	subs, err := s.subs.ObtainRateUserSubscriptionsByUserID(ctx, domain.UserTypeTelegram, userID)
	if err != nil {
		return nil, err
	}

	empty := &MeHistoryResult{Pair: pair, Total: 0, Items: []MeHistoryRowResult{}}
	if len(subs) == 0 {
		return empty, nil
	}

	sourceNames := uniqueSourceNames(subs)
	sourceMeta, err := s.sources.ObtainRateSourcesByNames(ctx, sourceNames)
	if err != nil {
		return nil, err
	}

	// Build SourcePairKey list filtered to sources whose canonical pair label
	// matches the requested pair. Uses the same pairGroupKey helper as ObtainMeChart.
	seenSources := make(map[string]struct{}, len(subs))
	var matchingKeys []domain.SourcePairKey
	for _, sub := range subs {
		src, ok := sourceMeta[sub.SourceName]
		if !ok {
			continue
		}
		label := pairGroupKey(src.BaseCurrency, src.QuoteCurrency)
		if label != strings.ToUpper(pair) {
			continue
		}
		// Dedup by source name at the SQL level: each distinct source_name appears
		// once in the tuple-IN clause regardless of subscription count. Do NOT dedup
		// by title here — sibling sources (same title, different source_name) must
		// both appear in matchingKeys so the SQL WHERE covers both.
		if _, already := seenSources[sub.SourceName]; already {
			continue
		}
		seenSources[sub.SourceName] = struct{}{}
		matchingKeys = append(matchingKeys, domain.SourcePairKey{
			SourceName:    src.Name,
			BaseCurrency:  src.BaseCurrency,
			QuoteCurrency: src.QuoteCurrency,
			Kind:          src.Kind,
		})
	}

	// Apply optional source-title filter. The comparison is byte-for-byte because
	// titles are stored case-sensitively in rate_sources.title.
	if sourceTitle != "" {
		// Filter in-place; matchingKeys is local to this call so reusing the backing array is safe.
		filtered := matchingKeys[:0]
		for _, k := range matchingKeys {
			if src, ok := sourceMeta[k.SourceName]; ok && src.Title == sourceTitle {
				filtered = append(filtered, k)
			}
		}
		matchingKeys = filtered
	}

	if len(matchingKeys) == 0 {
		return empty, nil
	}

	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	rawRows, rowTotal, groupedTotal, err := s.history.ObtainHistoryForPairsPaged(ctx, matchingKeys, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("ObtainMeHistory user=%s pair=%s: %w", userID, pair, err)
	}
	// rowTotal is the row-level count (one per rate_values row); groupedTotal is
	// the user-facing pagination total after BID/ASK sibling collapse.
	_ = rowTotal

	if len(rawRows) == 0 && groupedTotal == 0 {
		return empty, nil
	}

	// Build source-metadata maps for fast lookup during grouping.
	titleBySource := make(map[string]string, len(matchingKeys))
	kindBySource := make(map[string]domain.RateSourceKind, len(matchingKeys))
	for _, k := range matchingKeys {
		if src, ok := sourceMeta[k.SourceName]; ok {
			titleBySource[k.SourceName] = src.Title
		}
		kindBySource[k.SourceName] = k.Kind
	}

	// Group rows by (source_title, timestamp). The repository returns rows sorted
	// newest-first; we rely on that order when building the grouped list so the
	// result stays newest-first without an extra sort pass.
	type groupKey struct {
		sourceTitle string
		timestamp   time.Time
	}
	groupOrder := make([]groupKey, 0, len(rawRows))
	groupMap := make(map[groupKey]*MeHistoryRowResult, len(rawRows))

	for i := range rawRows {
		rv := &rawRows[i]
		title := titleBySource[rv.SourceName]
		gk := groupKey{sourceTitle: title, timestamp: rv.Timestamp}
		g, exists := groupMap[gk]
		if !exists {
			newRow := &MeHistoryRowResult{
				SourceTitle: title,
				Timestamp:   rv.Timestamp,
			}
			groupMap[gk] = newRow
			groupOrder = append(groupOrder, gk)
			g = newRow
		}
		// Assign price to the correct direction slot.
		kind := kindBySource[rv.SourceName]
		price := rv.Price
		if kind == domain.RateSourceKindBID {
			// Invariant 1: at most one BID source per (title, base, quote). If violated,
			// last-write-in-page-order wins silently — see warn-log below.
			if g.Bid != nil {
				log.Printf("warn: history title collision user=%s pair=%s title=%q ts=%s kind=BID: silent overwrite (Invariant 1 violated)",
					userID, pair, gk.sourceTitle, gk.timestamp.Format(time.RFC3339))
			}
			g.Bid = &price
		} else {
			// Invariant 1: at most one ASK source per (title, base, quote). If violated,
			// last-write-in-page-order wins silently — see warn-log below.
			if g.Ask != nil {
				log.Printf("warn: history title collision user=%s pair=%s title=%q ts=%s kind=ASK: silent overwrite (Invariant 1 violated)",
					userID, pair, gk.sourceTitle, gk.timestamp.Format(time.RFC3339))
			}
			g.Ask = &price
		}
	}

	items := make([]MeHistoryRowResult, 0, len(groupOrder))
	for _, gk := range groupOrder {
		items = append(items, *groupMap[gk])
	}

	// Compute per-direction deltas within the page. Results are grouped and
	// ordered newest-first. For delta computation we need oldest-first order per
	// provider, so we build a per-title slice in reverse, compute deltas, then
	// write them back into the items slice.
	//
	// v1 known limitation: the first row in any (title, direction) chain within
	// the page has nil delta even though the full history may have a predecessor.
	// Cross-page anchoring is deferred.
	type directionState struct {
		lastBid *float64
		lastAsk *float64
	}

	// Build per-title ordered indices (largest index = oldest in newest-first list).
	titleIndices := make(map[string][]int, len(matchingKeys))
	for i := range items {
		t := items[i].SourceTitle
		titleIndices[t] = append(titleIndices[t], i)
	}

	for _, idxList := range titleIndices {
		// Items are in newest-first order; sort descending by index to process
		// oldest-first when computing forward deltas.
		sort.Slice(idxList, func(a, b int) bool { return idxList[a] > idxList[b] })

		st := &directionState{}
		for _, idx := range idxList {
			row := &items[idx]
			if row.Bid != nil {
				if st.lastBid != nil && *st.lastBid != 0 {
					d := (*row.Bid - *st.lastBid) / *st.lastBid * 100
					row.BidDeltaPct = &d
				}
				v := *row.Bid
				st.lastBid = &v
			}
			if row.Ask != nil {
				if st.lastAsk != nil && *st.lastAsk != 0 {
					d := (*row.Ask - *st.lastAsk) / *st.lastAsk * 100
					row.AskDeltaPct = &d
				}
				v := *row.Ask
				st.lastAsk = &v
			}
		}
	}

	return &MeHistoryResult{
		Pair:  pair,
		Total: groupedTotal,
		Items: items,
	}, nil
}
