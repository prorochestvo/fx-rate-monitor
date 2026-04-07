package api

import (
	"context"
	"errors"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
)

func NewWebRestAPI(
	rExecutionHistory executionHistoryRepository,
	rRateSource rateSourceRepository,
	rRateValue rateValueRepository,
	rRateUserSubscription rateUserSubscriptionRepository,
	rRateUserEvent rateUserEventRepository,
) (*RateService, error) {

	h := &RateService{
		executionHistoryRepository:     rExecutionHistory,
		rateSourceRepository:           rRateSource,
		rateValueRepository:            rRateValue,
		rateUserSubscriptionRepository: rRateUserSubscription,
		rateUserEventRepository:        rRateUserEvent,
	}

	return h, nil
}

// RateService groups all v1 HTTP handlers and their repository dependencies.
type RateService struct {
	executionHistoryRepository     executionHistoryRepository
	rateSourceRepository           rateSourceRepository
	rateValueRepository            rateValueRepository
	rateUserSubscriptionRepository rateUserSubscriptionRepository
	rateUserEventRepository        rateUserEventRepository
}

func (h *RateService) ObtainLastNExecutionHistoryBySourceName(ctx context.Context, name string, limit int64) ([]domain.ExecutionHistory, error) {
	items, err := h.executionHistoryRepository.ObtainLastNExecutionHistoryBySourceName(ctx, name, limit, false)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateService) ObtainLastSuccessNExecutionHistoryBySourceName(ctx context.Context, name string, limit int64) ([]domain.ExecutionHistory, error) {
	items, err := h.executionHistoryRepository.ObtainLastNExecutionHistoryBySourceName(ctx, name, limit, true)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateService) ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error) {
	items, err := h.rateSourceRepository.ObtainAllRateSources(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateService) ObtainLastNRateValuesBySourceName(ctx context.Context, name string, limit int64) ([]domain.RateValue, error) {
	items, err := h.rateValueRepository.ObtainLastNRateValuesBySourceName(
		ctx,
		name,
		limit,
	)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateService) ObtainListOfLastRateUserEvent(ctx context.Context, limit int64) ([]domain.RateUserEvent, error) {
	items, err := h.rateUserEventRepository.ObtainLastNRateUserEvents(
		ctx,
		0,
		limit,
		domain.RateUserEventStatusPending,
		domain.RateUserEventStatusSent,
		domain.RateUserEventStatusFailed,
		domain.RateUserEventStatusCanceled,
	)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateService) ObtainFailedListOfRateUserEvent(ctx context.Context, offset, limit int64) ([]domain.RateUserEvent, error) {
	items, err := h.rateUserEventRepository.ObtainLastNRateUserEvents(
		ctx,
		offset,
		limit,
	)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

type executionHistoryRepository interface {
	ObtainLastNExecutionHistoryBySourceName(context.Context, string, int64, bool) ([]domain.ExecutionHistory, error)
}

type rateSourceRepository interface {
	ObtainAllRateSources(context.Context) ([]domain.RateSource, error)
}

type rateValueRepository interface {
	ObtainLastNRateValuesBySourceName(context.Context, string, int64) ([]domain.RateValue, error)
}

type rateUserSubscriptionRepository interface {
	ObtainRateUserSubscriptionsBySource(context.Context, string) ([]domain.RateUserSubscription, error)
}

type rateUserEventRepository interface {
	ObtainLastNRateUserEvents(context.Context, int64, int64, ...domain.RateUserEventStatus) ([]domain.RateUserEvent, error)
}
