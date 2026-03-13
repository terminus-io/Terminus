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
	"github.com/terminus-io/Terminus/pkg/utils"
	terminus_quota "github.com/terminus-io/quota"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	DiskAnnotation      = "storage.terminus.io/size"
	ContainerdBasePath  = "/run/containerd/io.containerd.runtime.v2.task/k8s.io/"
	SystemMountInfoFile = "/proc/1/mountinfo"
	ProjectIDAnnotation = "storage.terminus.io/project-id"
	quotaEnableLabel    = "storage.terminus.io/quota"
	KB                  = 1024
	MB                  = 1024 * KB
)

// StorageHook 负责处理磁盘限额
type StorageHook struct {
	containerdRootPath string
	containerdClient   *utils.ContainerdClientWrapper
	containerdCtx      context.Context
	store              *metadata.AsyncStore
	kClient            kubernetes.Interface
}

// qm quota.QuotaManager,
func NewStorageHook(store *metadata.AsyncStore, kClient kubernetes.Interface, containerdRootPath string, wrapper *utils.ContainerdClientWrapper, containerdCtx context.Context) nri.Hook {
	return &StorageHook{
		containerdRootPath: containerdRootPath,
		containerdClient:   wrapper,
		containerdCtx:      containerdCtx,
		store:              store,
		kClient:            kClient,
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
			return nil
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

	if pod.GetRuntimeHandler() == "io.containerd.kata.v2" || pod.GetRuntimeHandler() == "kata" {

		klog.Infof("[kata-container] Start container %s (ID: %s)", container.Name, container.Id)

		cont, err := h.containerdClient.LoadContainer(h.containerdCtx, container.Id)
		if err != nil {
			klog.ErrorS(err, "[kata-container] failed to load container", "containerID", container.Id)
			return nil
		}

		info, err := cont.Info(h.containerdCtx)
		if err != nil {
			klog.ErrorS(err, "[kata-container] failed to get container info", "containerID", container.Id)
			return nil
		}

		snapshotKey := info.SnapshotKey
		if snapshotKey == "" {
			klog.ErrorS(err, "[kata-container] container has no snapshot key", "containerID", container.Id)
			return nil
		}

		snapshotterName := info.Snapshotter
		if snapshotterName == "" {
			klog.ErrorS(err, "[kata-container] container has no snapshotter", "containerID", container.Id)
			return nil
		}

		snapshotter := h.containerdClient.SnapshotService(snapshotterName)
		if snapshotter == nil {
			klog.ErrorS(err, "[kata-container] snapshotter not found", "snapshotterName", snapshotterName)
			return nil
		}

		mounts, err := snapshotter.Mounts(h.containerdCtx, snapshotKey)
		if err != nil {
			klog.ErrorS(err, "[kata-container] failed to get mounts for snapshot", "snapshotKey", snapshotKey)
			return nil
		}

		if len(mounts) == 0 {
			klog.ErrorS(err, "[kata-container] no mounts returned for snapshot", "snapshotKey", snapshotKey)
			return nil
		}

		for _, m := range mounts {

			upperdir := findOptionValue(m.Options, "upperdir")

			if upperdir != "" {
				klog.V(4).Infof("[kata-container] Writable upperdir: %s\n", upperdir)
			}
			workdir := findOptionValue(m.Options, "workdir")

			if workdir != "" {
				klog.V(4).Infof("[kata-container] Workdir   : %s\n", workdir)
			}

			snapshotIDStr := filepath.Base(filepath.Dir(workdir))
			snapshotID, err := strconv.ParseUint(snapshotIDStr, 10, 32)
			if err != nil {
				klog.Errorf("[kata-container] failed to parse snapshot ID %q: %v", snapshotIDStr, err)
			}

			klog.V(2).Infof("[kata-container] Applying quota %d MB to container %s (ID: %s) at %s", limitBytes/MB, container.Name, container.Id, upperdir)

			klog.V(2).Infof("[kata-container] Target Quota Path: %s, Quota ProjectID: %d", upperdir, int(snapshotID))

			if err := terminus_quota.SetProjectID(upperdir, int(snapshotID)); err != nil {
				klog.Errorf("[kata-container] Failed to set fs project id for %s: %v", upperdir, err)
			}

			klog.V(2).Infof("[kata-container] Target Quota Path: %s, Quota ProjectID: %d", workdir, int(snapshotID))

			if err := terminus_quota.SetProjectIDRecursive(workdir, int(snapshotID)); err != nil {
				klog.Errorf("[kata-container] Failed to set work project id for %s: %v", workdir, err)
			}

			if err := terminus_quota.SetQuota(h.containerdRootPath, uint32(snapshotID),
				terminus_quota.ProjQuota, limitBytes/KB, 0, 0, 0); err != nil {
				klog.Errorf("[kata-container] Failed to apply quota: %v", err)
			}

			h.store.TriggerUpdate(uint32(snapshotID), metadata.ContainerInfo{
				ProjectID:     uint32(snapshotID),
				Namespace:     pod.Namespace,
				PodName:       pod.Name,
				ContainerName: container.Name,
				VolumeName:    "rootfs",
				StorageType:   metadata.ROOTFS_TYPE,
			})

			if err := h.handleUpdatePod(ctx, pod.Name, pod.Namespace, container.Name, fmt.Sprintf("%d", uint32(snapshotID))); err != nil {
				klog.Warningf("[kata-container] %s/%s pod label update failed, It may affect the reporting of pod disk monitoring metrics, err: %v",
					pod.Namespace, pod.Name, err)
			}
		}

		return nil
	}

	rootfsPath := filepath.Join(ContainerdBasePath, container.Id, "rootfs")
	runPath := filepath.Join(ContainerdBasePath, container.Id, "rootfs")
	//Obtain the snapshot ID of overlays as the ProjectID of xfs_quota
	snapshotID, foundPath, err := getOverlayPath(runPath)
	if err == nil && foundPath != "" {
		rootfsPath = foundPath
	} else {
		klog.Errorf("Could not find physical path for container %s", container.Id)
		return nil
	}

	klog.V(2).Infof("Applying quota %d MB to container %s (ID: %s) at %s", limitBytes/MB, container.Name, container.Id, rootfsPath)

	klog.V(2).Infof("Target Quota Path: %s, Quota ProjectID: %v", rootfsPath, snapshotID)

	if err := terminus_quota.SetProjectIDRecursive(rootfsPath, int(snapshotID)); err != nil {
		klog.Errorf("Failed to set fs project id for %s: %v", rootfsPath, err)
	}

	workPath := strings.TrimSuffix(rootfsPath, "/fs") + "/work"
	klog.V(2).Infof("Target Quota Path: %s, Quota ProjectID: %v", workPath, snapshotID)
	if err := terminus_quota.SetProjectIDRecursive(workPath, int(snapshotID)); err != nil {
		klog.Errorf("Failed to set work project id for %s: %v", workPath, err)
	}

	if err := terminus_quota.SetQuota(h.containerdRootPath, uint32(snapshotID),
		terminus_quota.ProjQuota, limitBytes/KB, 0, 0, 0); err != nil {
		klog.Errorf("Failed to apply quota: %v", err)
	}

	h.store.TriggerUpdate(uint32(snapshotID), metadata.ContainerInfo{
		ProjectID:     uint32(snapshotID),
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: container.Name,
		VolumeName:    "rootfs",
		StorageType:   metadata.ROOTFS_TYPE,
	})

	if err := h.handleUpdatePod(ctx, pod.Name, pod.Namespace, container.Name, fmt.Sprintf("%d", uint32(snapshotID))); err != nil {
		klog.Warningf("%s/%s pod label update failed, It may affect the reporting of pod disk monitoring metrics, err: %v",
			pod.Namespace, pod.Name, err)
	}
	return nil
}

func (h *StorageHook) Stop(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {

	prefix := DiskAnnotation + "." + container.Name
	_, ok := pod.Annotations[prefix]
	if !ok {
		_, ok = pod.Annotations[DiskAnnotation]
		if !ok {
			return nil
		}
	}

	if pod.GetRuntimeHandler() == "io.containerd.kata.v2" || pod.GetRuntimeHandler() == "kata" {
		klog.Infof("[kata-container] Stop container %s (ID: %s)", container.Name, container.Id)

		cont, err := h.containerdClient.LoadContainer(h.containerdCtx, container.Id)
		if err != nil {
			klog.ErrorS(err, "[kata-container] failed to load container", "containerID", container.Id)
			return nil
		}

		info, err := cont.Info(h.containerdCtx)
		if err != nil {
			klog.ErrorS(err, "[kata-container] failed to get container info", "containerID", container.Id)
			return nil
		}

		snapshotKey := info.SnapshotKey
		if snapshotKey == "" {
			klog.ErrorS(err, "[kata-container] container has no snapshot key", "containerID", container.Id)
			return nil
		}

		snapshotterName := info.Snapshotter
		if snapshotterName == "" {
			klog.ErrorS(err, "[kata-container] container has no snapshotter", "containerID", container.Id)
			return nil
		}

		snapshotter := h.containerdClient.SnapshotService(snapshotterName)
		if snapshotter == nil {
			klog.ErrorS(err, "[kata-container] snapshotter not found", "snapshotterName", snapshotterName)
			return nil
		}

		mounts, err := snapshotter.Mounts(h.containerdCtx, snapshotKey)
		if err != nil {
			klog.ErrorS(err, "[kata-container] failed to get mounts for snapshot", "snapshotKey", snapshotKey)
			return nil
		}

		if len(mounts) == 0 {
			klog.ErrorS(err, "[kata-container] no mounts returned for snapshot", "snapshotKey", snapshotKey)
			return nil
		}

		for _, m := range mounts {

			upperdir := findOptionValue(m.Options, "upperdir")

			if upperdir != "" {
				klog.V(4).Infof("[kata-container] Writable upperdir: %s\n", upperdir)
			}
			workdir := findOptionValue(m.Options, "workdir")

			if workdir != "" {
				klog.V(4).Infof("[kata-container] Workdir   : %s\n", workdir)
			}

			snapshotIDStr := filepath.Base(filepath.Dir(workdir))
			snapshotID, err := strconv.ParseUint(snapshotIDStr, 10, 32)
			if err != nil {
				klog.Errorf("[kata-container] failed to parse snapshot ID %q: %v", snapshotIDStr, err)
			}

			klog.V(2).Infof("[kata-container] Deleting quota to container %s (ID: %s) at %s", container.Name, container.Id, upperdir)

			if err := terminus_quota.SetProjectID(upperdir, int(snapshotID)); err != nil {
				klog.Errorf("[kata-container] Failed to set fs project id for %s: %v", upperdir, err)
			}

			klog.V(2).Infof("[kata-container] Target Quota Path: %s, Quota ProjectID: %d", workdir, int(snapshotID))

			if err := terminus_quota.RemoveQuota(h.containerdRootPath, uint32(snapshotID), terminus_quota.ProjQuota); err != nil {
				klog.Warningf("[kata-container] remove Project ID quota for %s, failed", upperdir)
				return err
			}

			if err := terminus_quota.ClearProjectID(h.containerdRootPath); err != nil {
				klog.Warningf("[kata-container] clear Project ID for %s, failed", upperdir)
			}

			h.store.TriggerDelete(uint32(snapshotID))
		}

		return nil
	}

	rootfsPath := filepath.Join(ContainerdBasePath, container.Id, "rootfs")
	klog.V(2).Infof("Deleting quota to container %s (ID: %s) at %s", container.Name, container.Id, rootfsPath)
	snapshotID, foundPath, err := getOverlayPath(rootfsPath)
	if err != nil {
		klog.Warningf("found Project ID for %s, failed", foundPath)
		return err
	}

	if err := terminus_quota.RemoveQuota(h.containerdRootPath, uint32(snapshotID), terminus_quota.ProjQuota); err != nil {
		klog.Warningf("remove Project ID quota for %s, failed", foundPath)
		return err
	}

	if err := terminus_quota.ClearProjectID(h.containerdRootPath); err != nil {
		klog.Warningf("clear Project ID for %s, failed", foundPath)
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

func findOptionValue(opts []string, key string) string {
	prefix := key + "="
	for _, opt := range opts {
		if strings.HasPrefix(opt, prefix) {
			return strings.TrimPrefix(opt, prefix)
		}
	}
	return ""
}
