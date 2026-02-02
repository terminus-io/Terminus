package exporter

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Frank-svg-dev/Terminus/pkg/metadata"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

func StartMetricsServer(ctx context.Context, collector prometheus.Collector, store *metadata.AsyncStore, metricsAddr string) error {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collector)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	srv := &http.Server{Addr: metricsAddr, Handler: mux}

	go func() {
		<-ctx.Done()
		klog.Info("Shutting down metrics server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	klog.InfoS("Listening metrics", "address", metricsAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
