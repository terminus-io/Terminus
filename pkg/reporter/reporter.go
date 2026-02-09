package reporter

import (
	"context"
	"time"

	"github.com/terminus-io/Terminus/pkg/metadata"
	"github.com/terminus-io/Terminus/pkg/utils"
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
		diskTotal, err := utils.GetDiskUsage("/var/lib/containerd")
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
			if err := r.ResetReportAnnotation(); err != nil {
				klog.Warningf("Reset Node Annotation failed, err: %v", err)
			}
			klog.Info("Reporter context cancelled, stopping loop.")
			return

		case <-ticker.C:
			reportFunc()
		}
	}

}
