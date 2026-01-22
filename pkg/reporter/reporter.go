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

		if err := r.ReportToAnnotation(ctx, diskTotal.Total); err != nil {
			klog.Warningf("Failed to report annotation: %v", err)
		} else {
			klog.V(4).InfoS("Successfully reported node stats", "total", diskTotal.Total)
		}
	}

	// 1. 启动时立即执行一次 (不要傻等30秒)
	reportFunc()

	// 2. 进入死循环
	for {
		select {
		// 监听 Context 取消信号 (比如主进程退出时)
		case <-ctx.Done():
			klog.Info("Reporter context cancelled, stopping loop.")
			return

		// 监听定时器信号
		case <-ticker.C:
			reportFunc()
		}
	}

}
