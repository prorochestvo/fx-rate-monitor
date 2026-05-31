package chart

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// HistoryValuesLoader loads paginated rate_values for a bulk set of
// (source, base, quote) keys, sorted newest first.
type HistoryValuesLoader interface {
	ObtainHistoryForPairsPaged(
		ctx context.Context, pairs []domain.SourcePairKey, limit, offset int64,
	) ([]domain.RateValue, int64, error)
}

// MeHistoryResult is the result of ObtainMeHistory: one page of rate-collection
// events for a canonical pair, sorted newest first.
type MeHistoryResult struct {
	// Pair is the canonical pair label the results are scoped to (e.g. "USD/KZT").
	Pair string
	// Total is the unpaginated count of matching rows.
	Total int64
	// Items contains the page rows, newest first.
	Items []MeHistoryRowResult
}

// MeHistoryRowResult is one grouped rate-collection event for a single
// (source, timestamp) tuple. BID and ASK from the same scrape are collapsed
// into one row.
type MeHistoryRowResult struct {
	// SourceName is the internal name of the source that produced this row.
	SourceName string
	// SourceTitle is the human-readable title of the source.
	SourceTitle string
	// Timestamp is when the collector scraped this value.
	Timestamp time.Time
	// Bid is the BID price; nil when the source only tracks ASK.
	Bid *float64
	// Ask is the ASK price; nil when the source only tracks BID.
	Ask *float64
	// BidDeltaPct is the percent change from the previous BID observation in
	// this (source, direction) chain within the page. Nil for the first row.
	BidDeltaPct *float64
	// AskDeltaPct is the percent change from the previous ASK observation in
	// this (source, direction) chain within the page. Nil for the first row.
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
// The returned MeHistoryResult always groups BID and ASK observations for the
// same (source, timestamp) into one MeHistoryRowResult. Delta percent is
// computed against the previous sample for the same (source, direction) chain
// in the current page — the first row in any chain has nil delta (v1 known
// limitation: cross-page anchoring is deferred).
func (s *Service) ObtainMeHistory(ctx context.Context, userID, pair string, page, limit int64) (*MeHistoryResult, error) {
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
		// Dedup by source name: one key per source regardless of subscription count.
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

	if len(matchingKeys) == 0 {
		return empty, nil
	}

	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	rawRows, total, err := s.history.ObtainHistoryForPairsPaged(ctx, matchingKeys, limit, offset)
	if err != nil {
		return nil, err
	}

	// Build source title map for fast lookup.
	titleBySource := make(map[string]string, len(matchingKeys))
	kindBySource := make(map[string]domain.RateSourceKind, len(matchingKeys))
	for _, k := range matchingKeys {
		if src, ok := sourceMeta[k.SourceName]; ok {
			titleBySource[k.SourceName] = src.Title
		}
		kindBySource[k.SourceName] = k.Kind
	}

	// Group rows by (source_name, timestamp). The repository returns rows sorted
	// newest-first; we rely on that order when building the grouped list so the
	// result stays newest-first without an extra sort pass.
	type groupKey struct {
		sourceName string
		timestamp  time.Time
	}
	groupOrder := make([]groupKey, 0, len(rawRows))
	groupMap := make(map[groupKey]*MeHistoryRowResult, len(rawRows))

	for i := range rawRows {
		rv := &rawRows[i]
		gk := groupKey{sourceName: rv.SourceName, timestamp: rv.Timestamp}
		g, exists := groupMap[gk]
		if !exists {
			newRow := &MeHistoryRowResult{
				SourceName:  rv.SourceName,
				SourceTitle: titleBySource[rv.SourceName],
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
			g.Bid = &price
		} else {
			g.Ask = &price
		}
	}

	items := make([]MeHistoryRowResult, 0, len(groupOrder))
	for _, gk := range groupOrder {
		items = append(items, *groupMap[gk])
	}

	// Compute per-direction deltas within the page. Results are grouped and
	// ordered newest-first. For delta computation we need oldest-first order per
	// source, so we build a per-source slice in reverse, compute deltas, then
	// write them back into the items slice.
	//
	// v1 known limitation: the first row in any (source, direction) chain within
	// the page has nil delta even though the full history may have a predecessor.
	// Cross-page anchoring is deferred.
	type directionState struct {
		lastBid *float64
		lastAsk *float64
	}

	// Build per-source ordered indices (largest index = oldest in newest-first list).
	sourceIndices := make(map[string][]int, len(matchingKeys))
	for i := range items {
		sn := items[i].SourceName
		sourceIndices[sn] = append(sourceIndices[sn], i)
	}

	for _, idxList := range sourceIndices {
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
		Total: total,
		Items: items,
	}, nil
}
