package ext4

import (
	"fmt"

	"github.com/Frank-svg-dev/Terminus/pkg/metadata"
	"github.com/Frank-svg-dev/Terminus/pkg/quota/ext4"
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

type Ext4Collector struct {
	mountPoint string
	exec       *ext4.Ext4CLI
	store      *metadata.AsyncStore
}

func NewExt4Collector(mountPoint string, store *metadata.AsyncStore) *Ext4Collector {
	return &Ext4Collector{
		mountPoint: mountPoint,
		exec:       ext4.NewExt4CLI(),
		store:      store,
	}
}

func (c *Ext4Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descBytesUsed
	ch <- descBytesLimit
	ch <- descInodesUsed
	ch <- descInodesLimit
}

func (c *Ext4Collector) Collect(ch chan<- prometheus.Metric) {
	blockReports, err := c.exec.FetchAllReports(c.mountPoint, "b")
	if err != nil {
		klog.ErrorS(err, "Failed to collect block metrics")
	} else {
		for id, r := range blockReports {
			containerInfo, ok := c.store.Get(id)
			if !ok {
				klog.Warning(id, "project ID is not found")
				continue
			}
			idStr := fmt.Sprintf("%d", id)
			ch <- prometheus.MustNewConstMetric(descBytesUsed, prometheus.GaugeValue, float64(r.Used),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, c.mountPoint, idStr)
			ch <- prometheus.MustNewConstMetric(descBytesLimit, prometheus.GaugeValue, float64(r.Limit),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, c.mountPoint, idStr)
		}
	}

	// 获取文件数数据 (Inodes)
	// 这里的 "i" 代表 Inode
	inodeReports, err := c.exec.FetchAllReports(c.mountPoint, "i")
	if err != nil {
		klog.ErrorS(err, "Failed to collect inode metrics")
	} else {
		for id, r := range inodeReports {
			containerInfo, ok := c.store.Get(id)
			if !ok {
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
