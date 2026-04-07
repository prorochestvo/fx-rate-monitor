package gateway

import (
	"errors"
	"net/http"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/application/api"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1"
)

func NewGateway(srvRate *api.RateService) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	mux, err := httpV1.NewRouter(mux, srvRate)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return mux, nil
}
