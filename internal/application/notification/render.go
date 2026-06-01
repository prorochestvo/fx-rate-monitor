package notification

import (
	"html"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// SubscriptionSnapshot pairs one subscription with the source metadata and the
// currently-known rate value needed to build a row in the digest. Sources with
// no current rate must be omitted by the caller — we cannot fabricate one.
type SubscriptionSnapshot struct {
	Subscription domain.RateUserSubscription
	Source       domain.RateSource
	CurrentPrice float64
}

// BuildSubscriptionDigest renders all caller subscriptions into one or more
// Telegram HTML message parts using the same aligned-table format the scheduled
// notifier produces. Cross-source dedup on (BaseCurrency, QuoteCurrency, Kind)
// matches RateCheckAgent.Run: BID keeps MAX price, ASK keeps MIN price, ties
// resolved by first-seen. The header is the plain "FX rates" title — no
// hashtag — because the digest is on-demand, not a trigger fire.
//
// Returns an empty slice when snapshots is empty (the caller is responsible
// for the "no subscriptions yet" empty-state UX).
func BuildSubscriptionDigest(now time.Time, loc *time.Location, snapshots []SubscriptionSnapshot) ([]string, error) {
	if len(snapshots) == 0 {
		return nil, nil
	}

	buckets := make(map[string]*dedupBucket, len(snapshots))
	for _, snap := range snapshots {
		foldIntoBuckets(buckets, snap)
	}

	alerts := make([]alert, 0, len(buckets))
	for _, b := range buckets {
		// Digest is on-demand, not a trigger fire — Triggers is nil (zero value)
		// so reasonHashtags returns "" and headerLines produces the bare header.
		alerts = append(alerts, b.a)
	}

	return buildAlertMessage(now, loc, alerts...)
}

// foldIntoBuckets inserts or updates a dedupBucket for the snapshot's (base, quote, kind)
// key, applying the BID-MAX / ASK-MIN extremum rule. Price and delta are always kept
// coherent: they are replaced together and always come from the same source.
// Strict > (BID) / < (ASK) means first-seen wins on equal prices, matching
// the behaviour in RateCheckAgent.Run.
//
// Trigger accumulation is intentionally absent — only the agent path collects
// triggers. The digest path calls this helper with no trigger side-effects.
func foldIntoBuckets(buckets map[string]*dedupBucket, snap SubscriptionSnapshot) {
	key := snap.Source.BaseCurrency + "|" + snap.Source.QuoteCurrency + "|" + string(snap.Source.Kind)
	delta := snap.CurrentPrice - snap.Subscription.LatestNotifiedRate

	b := buckets[key]
	if b == nil {
		b = &dedupBucket{
			a: alert{
				SourceName:    html.EscapeString(snap.Source.Name),
				BaseCurrency:  snap.Source.BaseCurrency,
				QuoteCurrency: snap.Source.QuoteCurrency,
				CurrencyKind:  snap.Source.Kind,
				CurrentPrice:  snap.CurrentPrice,
				Delta:         delta,
			},
			triggers: make(map[domain.SubscriptionConditionType]string),
		}
		buckets[key] = b
		return
	}

	// Extremum selection: BID keeps MAX price, ASK keeps MIN price.
	// Price and delta are replaced together (coherence invariant).
	// SourceName tracks whichever source holds the winning price.
	switch snap.Source.Kind {
	case domain.RateSourceKindBID:
		if snap.CurrentPrice > b.a.CurrentPrice {
			b.a.CurrentPrice = snap.CurrentPrice
			b.a.Delta = delta
			b.a.SourceName = html.EscapeString(snap.Source.Name)
		}
	case domain.RateSourceKindASK:
		if snap.CurrentPrice < b.a.CurrentPrice {
			b.a.CurrentPrice = snap.CurrentPrice
			b.a.Delta = delta
			b.a.SourceName = html.EscapeString(snap.Source.Name)
		}
	}
}
