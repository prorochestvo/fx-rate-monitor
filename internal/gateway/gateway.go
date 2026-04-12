package gateway

import (
	"errors"
	"net/http"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/application/service"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1"
)

func NewGateway(srvRateRestApi *service.RateRestApi) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	mux, err := httpV1.NewRouter(mux, srvRateRestApi)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	return mux, nil
}
