/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package metrics

import (
	"context"
	"net"
	"net/http"
	"time"

	imetrics "github.com/voedger/voedger/pkg/metrics"
	router2 "github.com/voedger/voedger/pkg/router"
	coreutils "github.com/voedger/voedger/pkg/utils"
)

func ProvideMetricsService(vvmCtx context.Context, metricsServicePort MetricsServicePort, imetrics imetrics.IMetrics) MetricsService {
	listener, err := net.Listen("tcp", coreutils.ServerAddress(int(metricsServicePort)))
	if err != nil {
		panic(err)
	}

	return &metricsService{
		Server: &http.Server{
			Handler: provideHandler(imetrics),
			BaseContext: func(l net.Listener) context.Context {
				return vvmCtx
			},
			ReadHeaderTimeout: router2.DefaultRouterReadTimeout * time.Second, // avoiding potential Slowloris attack (G112 linter rule)
		},
		listener: listener,
	}
}
