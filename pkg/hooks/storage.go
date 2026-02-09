package hooks

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/containerd/nri/pkg/api"
	"github.com/terminus-io/Terminus/pkg/metadata"
	"github.com/terminus-io/Terminus/pkg/nri"
	"github.com/terminus-io/Terminus/pkg/quota"
	"github.com/terminus-io/Terminus/pkg/utils"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	DiskAnnotation         = "storage.terminus.io/size"
	ContainerdBasePath     = "/run/containerd/io.containerd.runtime.v2.task/k8s.io/"
	ContainerdRootPath     = "/var/lib/containerd"
	SystemMountInfoFile    = "/proc/1/mountinfo"
	ProjectIDAnnotation    = "storage.terminus.io/project-id"
	quotaEnableLabel       = "storage.terminus.io/quota"
	defaultDiskSize        = "2Gi"
	defaultSideCarDiskSize = "500Mi"
)

// StorageHook 负责处理磁盘限额
type StorageHook struct {
	qm      quota.QuotaManager
	store   *metadata.AsyncStore
	kClient kubernetes.Interface
}

func NewStorageHook(qm quota.QuotaManager, store *metadata.AsyncStore, kClient kubernetes.Interface) nri.Hook {
	return &StorageHook{
		qm:      qm,
		store:   store,
		kClient: kClient,
	}
}

func (h *StorageHook) Name() string { return "StorageQuota" }

func (h *StorageHook) Process(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	return nil
}

func (h *StorageHook) Start(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {

	prefix := DiskAnnotation + "." + container.Name
	limitStr, ok := pod.Annotations[prefix]
	if !ok {
		limitStr, ok = pod.Annotations[DiskAnnotation]
		if !ok {
			if utils.IsSidecar(container.Name) {
				limitStr = defaultSideCarDiskSize
			} else {
				limitStr = defaultDiskSize
			}
		}
	}

	q, err := resource.ParseQuantity(limitStr)
	if err != nil {
		klog.ErrorS(err, "Failed to parse limit string", "limit", limitStr)
		return nil
	}

	limitBytes := uint64(q.Value())

	klog.InfoS("Parsed quota limit",
		"raw", limitStr,
		"bytes", limitBytes,
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
		klog.Errorf("Failed to set fs project id: %v ", err)
	}

	workPath := strings.TrimSuffix(rootfsPath, "/fs") + "/work"
	if err := h.qm.SetProjectID(workPath, uint32(snapshotID)); err != nil {
		klog.Errorf("Failed to set work project id: %v  111", err)
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

	if err := h.handleUpdatePod(ctx, pod.Name, pod.Namespace, container.Name, fmt.Sprintf("%d", uint32(snapshotID))); err != nil {
		klog.Warningf("%s/%s pod label update failed, It may affect the reporting of pod disk monitoring metrics, err: %v",
			pod.Namespace, pod.Name, err)
	}
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
	if err := h.qm.RemoveQuota(ContainerdRootPath, uint32(snapshotID)); err != nil {
		klog.Warningf("remove Project ID quota for %s, failed", foundPath)
		return err
	}

	h.store.TriggerDelete(uint32(snapshotID))
	return nil
}

func (h *StorageHook) handleUpdatePod(ctx context.Context, podName, namespace, containerName, projectID string) error {
	containerAnnotation := fmt.Sprintf("%s.%s", ProjectIDAnnotation, containerName)
	patchPayload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				quotaEnableLabel: "enabled",
			},
			"annotations": map[string]string{
				containerAnnotation: projectID,
			},
		},
	}

	data, err := json.Marshal(patchPayload)
	if err != nil {
		return err
	}

	_, err = h.kClient.CoreV1().Pods(namespace).Patch(
		ctx,
		podName,
		types.MergePatchType,
		data,
		metav1.PatchOptions{},
	)
	return err
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
