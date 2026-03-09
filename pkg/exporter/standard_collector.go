package exporter

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/terminus-io/Terminus/pkg/metadata"
	terminus_quota "github.com/terminus-io/quota"
	"k8s.io/klog/v2"
)

var (
	// 空间指标
	descBytesUsed = prometheus.NewDesc(
		"terminus_storage_used_bytes",
		"Storage usage in bytes per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id", "volume_name", "storage_type"}, nil,
	)
	descBytesLimit = prometheus.NewDesc(
		"terminus_storage_limit_bytes",
		"Storage hard limit in bytes per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id", "volume_name", "storage_type"}, nil,
	)
	// Inode 指标
	descInodesUsed = prometheus.NewDesc(
		"terminus_storage_inodes_used",
		"Inode usage count per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id", "volume_name", "storage_type"}, nil,
	)
	descInodesLimit = prometheus.NewDesc(
		"terminus_storage_inodes_limit",
		"Inode hard limit count per project ID",
		[]string{"namespace", "pod", "container", "mount_point", "project_id", "volume_name", "storage_type"}, nil,
	)

	maxID = uint32(999999999)
)

type StandardCollector struct {
	mountPoint        string
	kubeletMountPoint string
	store             *metadata.AsyncStore
}

func NewStandardCollector(mountPoint, kubeletMountPoint string, store *metadata.AsyncStore) *StandardCollector {
	return &StandardCollector{
		mountPoint:        mountPoint,
		kubeletMountPoint: kubeletMountPoint,
		store:             store,
	}
}

func (c *StandardCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descBytesUsed
	ch <- descBytesLimit
	ch <- descInodesUsed
	ch <- descInodesLimit
}

func (c *StandardCollector) Collect(ch chan<- prometheus.Metric) {

	quotaInfos, err := terminus_quota.ListQuotas(c.mountPoint, terminus_quota.ProjQuota, maxID)
	if err != nil {
		klog.ErrorS(err, "Failed to list project quotas")
	} else {
		for _, r := range quotaInfos {
			containerInfo, ok := c.store.Get(r.ID)
			if !ok {
				continue
			}

			mountPoint := c.mountPoint
			if containerInfo.StorageType == metadata.EMPTYDIR_TYPE {
				mountPoint = c.kubeletMountPoint
			}

			idStr := fmt.Sprintf("%d", r.ID)
			ch <- prometheus.MustNewConstMetric(descBytesUsed, prometheus.GaugeValue, float64(r.CurrentBlocks*1024),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, mountPoint, idStr, containerInfo.VolumeName, string(containerInfo.StorageType))
			ch <- prometheus.MustNewConstMetric(descBytesLimit, prometheus.GaugeValue, float64(r.BlockHardLimit*1024),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, mountPoint, idStr, containerInfo.VolumeName, string(containerInfo.StorageType))
			ch <- prometheus.MustNewConstMetric(descInodesUsed, prometheus.GaugeValue, float64(r.CurrentInodes),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, mountPoint, idStr, containerInfo.VolumeName, string(containerInfo.StorageType))
			ch <- prometheus.MustNewConstMetric(descInodesLimit, prometheus.GaugeValue, float64(r.BlockHardLimit),
				containerInfo.Namespace, containerInfo.PodName, containerInfo.ContainerName, mountPoint, idStr, containerInfo.VolumeName, string(containerInfo.StorageType))
		}

	}
}
