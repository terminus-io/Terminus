package reporter

import (
	"context"
	"time"

	"github.com/Frank-svg-dev/Terminus/pkg/metadata"
	"github.com/Frank-svg-dev/Terminus/pkg/utils"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type reporter struct {
	store    *metadata.AsyncStore
	kClient  kubernetes.Interface
	Interval time.Duration
}

func NewReporter(store *metadata.AsyncStore, kClient kubernetes.Interface, interval time.Duration) *reporter {
	return &reporter{
		store:    store,
		kClient:  kClient,
		Interval: interval,
	}
}

func (r *reporter) Run(ctx context.Context) {

	klog.InfoS("Starting reporter loop", "interval", r.Interval)
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	reportFunc := func() {
		diskTotal, err := utils.GetDiskUsage("/")
		if err != nil {
			klog.Warningf("Failed to get disk usage: %v", err)
			return
		}

		if err := r.ReportToAnnotation(ctx, diskTotal.Used, diskTotal.Total); err != nil {
			klog.Warningf("Failed to report annotation: %v", err)
		} else {
			klog.V(4).InfoS("Successfully reported node stats", "total", diskTotal.Total)
		}
	}

	reportFunc()
	for {
		select {
		case <-ctx.Done():
			klog.Info("Reporter context cancelled, stopping loop.")
			return

		case <-ticker.C:
			reportFunc()
		}
	}

}
