// Package collection houses agents that drive periodic FX rate collection.
//
// Rule resolution priority (per source, per collector invocation):
//  1. If one or more active rows exist in extraction_rules for
//     (kind="rate", target=source.Name), those rules are used exclusively —
//     one rule per label, all-or-nothing. The inline source.Rules field is
//     ignored for any source that has at least one active extraction rule.
//  2. If no active extraction rule exists for the source, the collector falls
//     back to source.Rules (the hand-authored inline rules stored in the
//     rate_sources.rules JSON column).
//
// Partial coverage (some labels in extraction_rules, some inline) is treated
// as a configuration error: the operator must either add the missing labels to
// the targets file and re-run ruledoctor, or delete the partial active rows.
// This keeps the fallback logic unambiguous — a source has either migrated to
// the new table or it hasn't.
//
// This coexistence path lets operators promote sources to LLM-generated rules
// one at a time with zero downtime.  Phasing out inline rules entirely is a
// future plan.
package collection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/tools/rateextractor"
)

func NewRateAgent(
	proxyURL string,
	rRateSource rateSourceRepository,
	rExecutionHistory executionHistoryRepository,
	rRateValue rateValueRepository,
	rExtractionRule extractionRuleRepository,
	logger io.Writer,
) (*RateAgent, error) {
	extractor, err := rateextractor.NewRateExtractor(rRateValue, proxyURL, time.Minute, logger)
	if err != nil {
		return nil, err
	}

	a := &RateAgent{
		rateValueRepository:        rRateValue,
		rateSourceRepository:       rRateSource,
		executionHistoryRepository: rExecutionHistory,
		extractionRuleRepository:   rExtractionRule,
		rateExtractor:              extractor,
		logger:                     logger,
	}

	return a, nil
}

type RateAgent struct {
	rateValueRepository        rateValueRepository
	rateSourceRepository       rateSourceRepository
	executionHistoryRepository executionHistoryRepository
	extractionRuleRepository   extractionRuleRepository
	rateExtractor              rateExtractor
	logger                     io.Writer
}

func (a *RateAgent) Run(ctx context.Context) (err error) {
	// isDue returns true if the source should run in this invocation.
	// The grace period is interval/4, clamped to [30s, 1h], to absorb scheduling
	// jitter between sources that share the same declared interval. For example, a
	// 4h source uses a 1h grace (fires when elapsed >= 3h) so that two sources whose
	// last runs drifted by several minutes still land in the same invocation.
	// Short intervals (<= 2m) keep the original 30s grace unchanged.
	// If no successful execution history exists, the source is always considered due.
	isDue := func(
		ctx context.Context,
		repository executionHistoryRepository,
		sourceName string,
		interval time.Duration,
		now time.Time,
	) bool {
		records, err := repository.ObtainLastNExecutionHistoryBySourceName(ctx, sourceName, 1, true)
		if err != nil || len(records) == 0 {
			return true
		}
		grace := interval >> 2
		grace = max(grace, 30*time.Second)
		grace = min(grace, time.Hour)
		return now.Sub(records[0].Timestamp) >= interval-grace
	}

	var sources []domain.RateSource
	if s, errSource := a.rateSourceRepository.ObtainAllRateSources(ctx); errSource != nil {
		errSource = errors.Join(errSource, internal.NewTraceError())
		return errSource
	} else if len(s) > 0 {
		now := time.Now().UTC()
		sources = make([]domain.RateSource, 0, len(s))
		for _, source := range s {
			if !source.Active {
				continue
			}
			interval, errInterval := time.ParseDuration(source.Interval)
			if errInterval != nil {
				errInterval = fmt.Errorf("invalid interval %q, %s", source.Interval, errInterval.Error())
				errInterval = errors.Join(errInterval, internal.NewTraceError())
				err = errors.Join(err, errInterval)
				continue
			}
			if !isDue(ctx, a.executionHistoryRepository, source.Name, interval, now) {
				continue
			}
			sources = append(sources, source)
		}
	}

	if sources == nil || len(sources) == 0 {
		return
	}

	errs := a.execution(ctx, sources)

	combined := make([]error, 0, len(errs))
	for k, e := range errs {
		if e != nil {
			combined = append(combined, fmt.Errorf("source %s: %w", k, e))
		}
	}
	return errors.Join(err, errors.Join(combined...))
}

func (a *RateAgent) execution(ctx context.Context, sources []domain.RateSource) map[string]error {
	now := time.Now().UTC()
	errs := make(map[string]error, len(sources))

	for _, source := range sources {
		h := &domain.ExecutionHistory{
			SourceName: source.Name,
			Success:    true,
			Timestamp:  now,
		}

		rules, activeRules, err := a.resolveRules(ctx, source)
		if err != nil {
			h.Success = false
			h.Error = errors.Join(err, internal.NewTraceError()).Error()
			err = errors.Join(err, a.executionHistoryRepository.RetainExecutionHistory(ctx, h))
			if err != nil {
				errs[source.Name] = errors.Join(errs[source.Name], err)
			}
			continue
		}

		// Build a synthetic source with the resolved rules so we never mutate the original.
		synthetic := source
		synthetic.Rules = rules

		err = a.rateExtractor.Run(ctx, &synthetic)
		if err != nil {
			h.Success = false
			h.Error = errors.Join(err, internal.NewTraceError()).Error()
		} else {
			// Non-fatal: update LastVerifiedAt for every rule that produced a value.
			for _, ar := range activeRules {
				if touchErr := a.extractionRuleRepository.TouchVerifiedAt(ctx, ar.ID, now); touchErr != nil {
					log.Printf("rateagent: touch verified_at for rule %s: %v", ar.ID, touchErr)
				}
			}
		}

		err = errors.Join(err, a.executionHistoryRepository.RetainExecutionHistory(ctx, h))
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			errs[source.Name] = errors.Join(errs[source.Name], err)
		}
	}

	return errs
}

// resolveRules returns the rules to apply for a source run. When active rows
// exist in extraction_rules for (kind="rate", target=source.Name), all of them
// are returned (one per label) as the first value; the second value carries the
// originating ExtractionRule records so the caller can touch LastVerifiedAt on
// success. When no active rows exist, the source's inline Rules are returned
// and the second value is nil (all-or-nothing fallback; the two sets are never
// mixed).
func (a *RateAgent) resolveRules(ctx context.Context, source domain.RateSource) ([]domain.RateSourceRule, []domain.ExtractionRule, error) {
	active, err := a.extractionRuleRepository.ObtainActiveRulesByTarget(
		ctx, domain.ExtractionRuleKindRate, source.Name,
	)
	if err != nil {
		return nil, nil, errors.Join(err, internal.NewTraceError())
	}
	if len(active) == 0 {
		return source.Rules, nil, nil
	}
	out := make([]domain.RateSourceRule, 0, len(active))
	for _, r := range active {
		out = append(out, domain.RateSourceRule{
			Pair:    r.Label,
			Method:  r.Method,
			Pattern: r.Pattern,
		})
	}
	return out, active, nil
}

type rateExtractor interface {
	Run(context.Context, *domain.RateSource) error
}

type executionHistoryRepository interface {
	RetainExecutionHistory(context.Context, *domain.ExecutionHistory) error
	ObtainLastNExecutionHistoryBySourceName(context.Context, string, int64, bool) ([]domain.ExecutionHistory, error)
}

type rateSourceRepository interface {
	ObtainRateSourceByName(context.Context, string) (*domain.RateSource, error)
	ObtainAllRateSources(context.Context) ([]domain.RateSource, error)
}

type rateValueRepository interface {
	ObtainLastNRateValuesBySourceName(context.Context, string, int64) ([]domain.RateValue, error)
	RetainRateValue(context.Context, *domain.RateValue) error
}

type extractionRuleRepository interface {
	ObtainActiveRulesByTarget(ctx context.Context, kind domain.ExtractionRuleKind, targetID string) ([]domain.ExtractionRule, error)
	TouchVerifiedAt(ctx context.Context, ruleID string, when time.Time) error
}
