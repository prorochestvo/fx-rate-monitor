package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/application/service"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1"
)

// meSubscriptionRepo is a pass-through interface from the concrete repository layer.
type meSubscriptionRepo interface {
	ObtainRateUserSubscriptionsByUserID(ctx context.Context, userType domain.UserType, userID string) ([]domain.RateUserSubscription, error)
}

// meSourceRepo is a pass-through interface for source look-ups.
type meSourceRepo interface {
	ObtainRateSourceByName(ctx context.Context, name string) (*domain.RateSource, error)
}

// meRateValueRepo is a pass-through interface for rate value look-ups.
type meRateValueRepo interface {
	ObtainLastNRateValuesBySourceName(ctx context.Context, name string, limit int64) ([]domain.RateValue, error)
}

func NewGateway(
	srvRateRestApi *service.RateRestApi,
	botToken string,
	subRepo meSubscriptionRepo,
	sourceRepo meSourceRepo,
	rateValueRepo meRateValueRepo,
) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	mux, err := httpV1.NewRouter(mux, srvRateRestApi, botToken, subRepo, sourceRepo, rateValueRepo)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return mux, nil
}
