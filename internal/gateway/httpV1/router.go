package httpV1

import (
	"net/http"

	"github.com/seilbekskindirov/monitor/internal/application/service"
	v1 "github.com/seilbekskindirov/monitor/internal/gateway/httpV1/handlers"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/routes"
)

func NewRouter(
	mux *http.ServeMux,
	srvRateRestApi *service.RateRestApi,
) (*http.ServeMux, error) {
	h, err := v1.NewHandler(srvRateRestApi)
	if err != nil {
		return nil, err
	}

	mux.HandleFunc("GET "+routes.Sources, h.ListSources)
	mux.HandleFunc("PATCH "+routes.SourceToggleActive, h.ToggleSourceActive)

	// SourceRatesChart must come before SourceRates: the chart path is more specific
	// and Go's ServeMux selects the longest-matching literal prefix first.
	mux.HandleFunc("GET "+routes.SourceRatesChart, h.GetRatesChart)
	mux.HandleFunc("GET "+routes.SourceRates, h.ListRates)
	mux.HandleFunc("GET "+routes.SourceHistory, h.ListHistory)
	mux.HandleFunc("GET "+routes.SourceEventsFailed, h.ListSourceFailedEvents)
	// SourceSubscriptionsList must come before SourceSubscriptions to avoid prefix clash.
	mux.HandleFunc("GET "+routes.SourceSubscriptionsList, h.ListSourceSubscriptionDetails)
	mux.HandleFunc("GET "+routes.SourceSubscriptions, h.ListSourceSubscriptions)
	mux.HandleFunc("GET "+routes.SourceEventsDaily, h.ListSourceDailyEvents)

	mux.HandleFunc("GET "+routes.Stats, h.ListStats)
	mux.HandleFunc("GET "+routes.ErrorsExecution, h.ListExecutionErrors)
	mux.HandleFunc("GET "+routes.EventsPending, h.ListPendingEvents)

	// NotificationsFailed must be registered before Notifications so that
	// ServeMux longest-prefix matching selects the correct handler.
	mux.HandleFunc("GET "+routes.NotificationsFailed, h.ListFailedNotifications)
	mux.HandleFunc("GET "+routes.Notifications, h.ListNotifications)

	return mux, nil
}
