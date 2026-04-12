package service

import (
	"context"
	"errors"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/repository"
)

func NewRateRestAPI(
	rExecutionHistory executionHistoryRepository,
	rRateSource rateSourceRepository,
	rRateValue rateValueRepository,
	rRateUserSubscription rateUserSubscriptionRepository,
	rRateUserEvent rateUserEventRepository,
) (*RateRestApi, error) {

	h := &RateRestApi{
		executionHistoryRepository:     rExecutionHistory,
		rateSourceRepository:           rRateSource,
		rateValueRepository:            rRateValue,
		rateUserSubscriptionRepository: rRateUserSubscription,
		rateUserEventRepository:        rRateUserEvent,
	}

	return h, nil
}

// RateRestApi groups all v1 HTTP handlers and their repository dependencies.
type RateRestApi struct {
	executionHistoryRepository     executionHistoryRepository
	rateSourceRepository           rateSourceRepository
	rateValueRepository            rateValueRepository
	rateUserSubscriptionRepository rateUserSubscriptionRepository
	rateUserEventRepository        rateUserEventRepository
}

func (h *RateRestApi) HealthCheck(ctx context.Context) error {
	return nil
}

func (h *RateRestApi) ObtainLastNExecutionHistoryBySourceName(ctx context.Context, name string, limit int64) ([]domain.ExecutionHistory, error) {
	items, err := h.executionHistoryRepository.ObtainLastNExecutionHistoryBySourceName(ctx, name, limit, false)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateRestApi) ObtainLastSuccessNExecutionHistoryBySourceName(ctx context.Context, name string, limit int64) ([]domain.ExecutionHistory, error) {
	items, err := h.executionHistoryRepository.ObtainLastNExecutionHistoryBySourceName(ctx, name, limit, true)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateRestApi) ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error) {
	items, err := h.rateSourceRepository.ObtainAllRateSources(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

func (h *RateRestApi) ObtainLastNRateValuesBySourceName(ctx context.Context, name string, limit int64) ([]domain.RateValue, error) {
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

func (h *RateRestApi) ObtainListOfLastRateUserEvent(ctx context.Context, limit int64) ([]domain.RateUserEvent, error) {
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

func (h *RateRestApi) ObtainFailedListOfRateUserEvent(ctx context.Context, offset, limit int64) ([]domain.RateUserEvent, error) {
	items, err := h.rateUserEventRepository.ObtainLastNRateUserEvents(
		ctx,
		offset,
		limit,
		domain.RateUserEventStatusFailed,
	)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return items, nil
}

// ObtainPendingRateUserEvents returns up to 1000 pending events (default-page widget).
func (h *RateRestApi) ObtainPendingRateUserEvents(ctx context.Context) ([]domain.RateUserEvent, error) {
	items, err := h.rateUserEventRepository.ObtainLastNRateUserEvents(
		ctx, 0, 1000, domain.RateUserEventStatusPending,
	)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}
	return items, nil
}

// ObtainRateValueChartBySourceName returns aggregated chart data for the given source and period.
func (h *RateRestApi) ObtainRateValueChartBySourceName(
	ctx context.Context, name string, period repository.ChartPeriod,
) ([]repository.ChartPoint, error) {
	items, err := h.rateValueRepository.ObtainRateValueChartBySourceName(ctx, name, period)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}
	return items, nil
}

// ObtainFailedRateUserEventsBySourceName returns a single page of failed events for a source.
func (h *RateRestApi) ObtainFailedRateUserEventsBySourceName(
	ctx context.Context, sourceName string, page, pageSize int64,
) ([]domain.RateUserEvent, error) {
	offset := (page - 1) * pageSize
	items, err := h.rateUserEventRepository.ObtainRateUserEventsBySourceName(
		ctx, sourceName, offset, pageSize, domain.RateUserEventStatusFailed,
	)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}
	return items, nil
}

// ObtainSubscriptionSummaryBySource returns grouped subscription + event statistics for a source.
func (h *RateRestApi) ObtainSubscriptionSummaryBySource(
	ctx context.Context, sourceName string,
) ([]repository.SubscriptionSummary, error) {
	items, err := h.rateUserSubscriptionRepository.ObtainSubscriptionSummaryBySource(ctx, sourceName)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
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
	ObtainRateValueChartBySourceName(context.Context, string, repository.ChartPeriod) ([]repository.ChartPoint, error)
}

type rateUserSubscriptionRepository interface {
	ObtainRateUserSubscriptionsBySource(context.Context, string) ([]domain.RateUserSubscription, error)
	ObtainSubscriptionSummaryBySource(context.Context, string) ([]repository.SubscriptionSummary, error)
}

type rateUserEventRepository interface {
	ObtainLastNRateUserEvents(context.Context, int64, int64, ...domain.RateUserEventStatus) ([]domain.RateUserEvent, error)
	ObtainRateUserEventsBySourceName(context.Context, string, int64, int64, ...domain.RateUserEventStatus) ([]domain.RateUserEvent, error)
}
