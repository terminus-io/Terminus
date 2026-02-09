package exporter

import (
	"fmt"

	"github.com/Frank-svg-dev/Terminus/pkg/metadata"
	"github.com/Frank-svg-dev/Terminus/pkg/quota/xfs"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
)

var (
	// 空间指标
	descBytesUsed = prometheus.NewDesc(
		"terminus_storage_used_bytes",
		"Storage usage in bytes per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id"}, nil,
	)
	descBytesLimit = prometheus.NewDesc(
		"terminus_storage_limit_bytes",
		"Storage hard limit in bytes per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id"}, nil,
	)
	// Inode 指标
	descInodesUsed = prometheus.NewDesc(
		"terminus_storage_inodes_used",
		"Inode usage count per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id"}, nil,
	)
	descInodesLimit = prometheus.NewDesc(
		"terminus_storage_inodes_limit",
		"Inode hard limit count per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id"}, nil,
	)
)

type XFSCollector struct {
	mountPoint string
	exec       *xfs.XFSCLI
	store      *metadata.AsyncStore
}

func NewXFSCollector(mountPoint string, store *metadata.AsyncStore) *XFSCollector {
	return &XFSCollector{
		mountPoint: mountPoint,
		exec:       xfs.NewXFSCLI(),
		store:      store,
	}
}

func (c *XFSCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descBytesUsed
	ch <- descBytesLimit
	ch <- descInodesUsed
	ch <- descInodesLimit
}

func (c *XFSCollector) Collect(ch chan<- prometheus.Metric) {
	blockReports, err := c.exec.FetchAllReports(c.mountPoint, "b")
	if err != nil {
		klog.ErrorS(err, "Failed to collect block metrics")
	} else {
		for id, r := range blockReports {
			containerInfo, ok := c.store.Get(id)
			if !ok {
				continue
			}
			idStr := fmt.Sprintf("%d", id)
			ch <- prometheus.MustNewConstMetric(descBytesUsed, prometheus.GaugeValue, float64(r.Used),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, c.mountPoint, idStr)
			ch <- prometheus.MustNewConstMetric(descBytesLimit, prometheus.GaugeValue, float64(r.Limit),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, c.mountPoint, idStr)
		}
	}

	inodeReports, err := c.exec.FetchAllReports(c.mountPoint, "i")
	if err != nil {
		klog.ErrorS(err, "Failed to collect inode metrics")
	} else {
		for id, r := range inodeReports {
			containerInfo, ok := c.store.Get(id)
			if !ok {
				klog.Warning(id, "project ID is not found")
				continue
			}
			idStr := fmt.Sprintf("%d", id)
			ch <- prometheus.MustNewConstMetric(descInodesUsed, prometheus.GaugeValue, float64(r.Used),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, c.mountPoint, idStr)
			ch <- prometheus.MustNewConstMetric(descInodesLimit, prometheus.GaugeValue, float64(r.Limit),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, c.mountPoint, idStr)
		}
	}
}
