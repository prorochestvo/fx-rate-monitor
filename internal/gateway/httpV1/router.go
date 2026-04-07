package httpV1

import (
	"net/http"

	"github.com/seilbekskindirov/monitor/internal/application/api"
	v1 "github.com/seilbekskindirov/monitor/internal/gateway/httpV1/handlers"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/routes"
)

func NewRouter(
	mux *http.ServeMux,
	srvRate *api.RateService,
) (*http.ServeMux, error) {
	h, err := v1.NewHandler(srvRate)
	if err != nil {
		return nil, err
	}

	mux.HandleFunc("GET "+routes.Sources, h.ListSources)
	mux.HandleFunc("GET "+routes.SourceRates, h.ListRates)
	mux.HandleFunc("GET "+routes.SourceHistory, h.ListHistory)
	// NotificationsFailed must be registered before Notifications so that
	// ServeMux longest-prefix matching selects the correct handler.
	mux.HandleFunc("GET "+routes.NotificationsFailed, h.ListFailedNotifications)
	mux.HandleFunc("GET "+routes.Notifications, h.ListNotifications)
	return mux, nil
}
