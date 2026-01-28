package hooks

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Frank-svg-dev/Terminus/pkg/metadata"
	"github.com/Frank-svg-dev/Terminus/pkg/nri"
	"github.com/Frank-svg-dev/Terminus/pkg/quota"
	"github.com/containerd/nri/pkg/api"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	DiskAnnotation      = "storage.terminus.io/size"
	ContainerdBasePath  = "/run/containerd/io.containerd.runtime.v2.task/k8s.io/"
	ContainerdRootPath  = "/var/lib/containerd"
	SystemMountInfoFile = "/proc/1/mountinfo"
)

// StorageHook 负责处理磁盘限额
type StorageHook struct {
	qm    quota.QuotaManager
	store *metadata.AsyncStore
}

// 构造函数：需要注入底层的 QuotaManager
func NewStorageHook(qm quota.QuotaManager, store *metadata.AsyncStore) nri.Hook {
	return &StorageHook{
		qm:    qm,
		store: store,
	}
}

func (h *StorageHook) Name() string { return "StorageQuota" }

func (h *StorageHook) Process(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	return nil
}

func (h *StorageHook) Start(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {

	limitStr, ok := pod.Annotations[DiskAnnotation]
	if !ok {
		return nil
		// limitStr, ok = m.namespace[pod.Namespace]
		// if !ok {
		// 	return nil
		// }
	}

	q, err := resource.ParseQuantity(limitStr)
	if err != nil {
		klog.ErrorS(err, "Failed to parse limit string", "limit", limitStr)
		return nil
	}

	// 3. 直接获取 byte 值 (int64)
	limitBytes := uint64(q.Value())

	klog.InfoS("Parsed quota limit",
		"raw", limitStr, // "10Gi"
		"bytes", limitBytes, // 10737418240
	)

	rootfsPath := filepath.Join(ContainerdBasePath, container.Id, "rootfs")

	klog.V(2).Infof("Applying quota %d MB to container %s (ID: %s) at %s", limitBytes, container.Name, container.Id, rootfsPath)

	runPath := filepath.Join(ContainerdBasePath, container.Id, "rootfs")
	//Obtain the snapshot ID of overlays as the ProjectID of xfs_quota
	snapshotID, foundPath, err := getOverlayPath(runPath)
	if err == nil && foundPath != "" {
		rootfsPath = foundPath
	} else {
		klog.Errorf("Could not find physical path for container %s", container.Id)
		return nil
	}

	klog.V(2).Infof("Target XFS Quota Path: %s, Quota ProjectID: %v", rootfsPath, snapshotID)

	if err := h.qm.SetProjectID(rootfsPath, uint32(snapshotID)); err != nil {
		klog.Errorf("Failed to apply quota: %v  11133", err)
	}

	workPath := strings.TrimSuffix(rootfsPath, "/fs") + "/work"
	if err := h.qm.SetProjectID(workPath, uint32(snapshotID)); err != nil {
		klog.Errorf("Failed to apply quota: %v  111", err)
	}

	if err := h.qm.SetQuota(uint32(snapshotID), limitBytes); err != nil {
		klog.Errorf("Failed to apply quota: %v", err)
	}

	h.store.TriggerUpdate(uint32(snapshotID), metadata.ContainerInfo{
		ProjectID:     uint32(snapshotID),
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: container.Name,
	})

	// if err := applyXFSQuota(snapshotID, rootfsPath, limitMB); err != nil {
	// 	klog.Errorf("Failed to apply quota: %v", err)
	// }
	return nil
}

func (h *StorageHook) Stop(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {

	_, ok := pod.Annotations[DiskAnnotation]
	if !ok {
		return nil
	}

	rootfsPath := filepath.Join(ContainerdBasePath, container.Id, "rootfs")
	klog.V(2).Infof("Deleting quota to container %s (ID: %s) at %s", container.Name, container.Id, rootfsPath)
	snapshotID, foundPath, err := getOverlayPath(rootfsPath)
	if err != nil {
		klog.Warningf("found Project ID for %s, failed", foundPath)
		return err
	}
	if err := h.qm.RemoveQuota("/", uint32(snapshotID)); err != nil {
		klog.Warningf("remove Project ID quota for %s, failed", foundPath)
		return err
	}

	h.store.TriggerDelete(uint32(snapshotID))
	return nil
}

func getOverlayPath(containerRootfs string) (uint64, string, error) {
	f, err := os.Open(SystemMountInfoFile)
	if err != nil {
		return 0, "", fmt.Errorf("failed to open host_mountinfo: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, containerRootfs) {
			continue
		}

		fields := strings.Split(line, " - ")
		if len(fields) < 2 {
			continue
		}

		preFields := strings.Fields(fields[0])
		mountPoint := preFields[4]

		if mountPoint != containerRootfs {
			continue
		}

		postFields := strings.Fields(fields[1])
		if len(postFields) < 3 {
			continue
		}
		options := postFields[2]
		for _, opt := range strings.Split(options, ",") {
			if strings.HasPrefix(opt, "upperdir=") {
				upperDir := strings.TrimPrefix(opt, "upperdir=")
				cleanPath := filepath.Clean(upperDir)
				if strings.HasSuffix(cleanPath, "/fs") {
					cleanPath = filepath.Dir(cleanPath)
				}
				snapshotId, err := strconv.ParseUint(filepath.Base(cleanPath), 10, 64)
				if err != nil {
					return 0, "", fmt.Errorf("failed to parse snapshot id from path [%s]: %v", upperDir, err)
				}

				return snapshotId, upperDir, nil
			}
		}
	}

	return 0, "", fmt.Errorf("overlay path not found in mountinfo for %s", containerRootfs)
}
