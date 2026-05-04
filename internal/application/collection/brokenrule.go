package collection

import (
	"context"
	"errors"
	"io"
	"log"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
)

const brokenRuleConsecutiveFailureThreshold = 3

const brokenRuleLogPrefix = "BrokenRulePromoter: "

type BrokenRulePromoter struct {
	ruleRepo    brokenRuleRuleRepository
	historyRepo brokenRuleHistoryRepository
	logger      *log.Logger
}

func NewBrokenRulePromoter(
	ruleRepo brokenRuleRuleRepository,
	historyRepo brokenRuleHistoryRepository,
	logger io.Writer,
) *BrokenRulePromoter {
	if logger == nil {
		logger = io.Discard
	}
	return &BrokenRulePromoter{
		ruleRepo:    ruleRepo,
		historyRepo: historyRepo,
		logger:      log.New(logger, brokenRuleLogPrefix, log.Lmsgprefix),
	}
}

func (p *BrokenRulePromoter) Run(ctx context.Context) error {
	activeRules, err := p.ruleRepo.ObtainAllActiveRules(ctx, domain.ExtractionRuleKindRate)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}

	for _, rule := range activeRules {
		rule := rule
		history, histErr := p.historyRepo.ObtainLastNExecutionHistoryBySourceName(
			ctx, rule.TargetID, int64(brokenRuleConsecutiveFailureThreshold), false,
		)
		if histErr != nil {
			p.logger.Printf("fetch history for %s: %v", rule.TargetID, histErr)
			continue
		}
		if len(history) < brokenRuleConsecutiveFailureThreshold {
			continue
		}
		if !allFailed(history) {
			continue
		}
		if markErr := p.ruleRepo.MarkRuleStatus(ctx, rule.ID, domain.ExtractionRuleStatusBroken); markErr != nil {
			p.logger.Printf("mark rule %s broken: %v", rule.ID, markErr)
			continue
		}
		p.logger.Printf("rule %s for source %s marked broken after %d consecutive failures",
			rule.ID, rule.TargetID, brokenRuleConsecutiveFailureThreshold)
	}

	return nil
}

// allFailed returns true only when every record has Success == false.
func allFailed(records []domain.ExecutionHistory) bool {
	if len(records) == 0 {
		return false
	}
	for _, r := range records {
		if r.Success {
			return false
		}
	}
	return true
}

type brokenRuleRuleRepository interface {
	ObtainAllActiveRules(ctx context.Context, kind domain.ExtractionRuleKind) ([]domain.ExtractionRule, error)
	MarkRuleStatus(ctx context.Context, id string, status domain.ExtractionRuleStatus) error
}

type brokenRuleHistoryRepository interface {
	ObtainLastNExecutionHistoryBySourceName(ctx context.Context, sourceName string, limit int64, successOnly bool) ([]domain.ExecutionHistory, error)
}
