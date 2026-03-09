package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/containerd/nri/pkg/api"
	"github.com/terminus-io/Terminus/pkg/metadata"
	"github.com/terminus-io/Terminus/pkg/nri"
	"github.com/terminus-io/Terminus/pkg/utils"
	terminus_quota "github.com/terminus-io/quota"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	EmptyDirPrjIDAnnotation = "emptydir.terminus.io/project-id"
	EmptyDirQuotaLabel      = "emptydir.terminus.io/quota"
)

// EmptyDirHook 负责处理 emptydir
type EmptyDirHook struct {
	kubeleRootPath string
	store          *metadata.AsyncStore
	kClient        kubernetes.Interface
}

func NewEmptyDirHook(store *metadata.AsyncStore, kClient kubernetes.Interface, kubeletRootPath string) nri.Hook {
	return &EmptyDirHook{
		kubeleRootPath: kubeletRootPath,
		store:          store,
		kClient:        kClient,
	}
}

func (h *EmptyDirHook) Name() string { return "EmptyDirQuota" }

func (h *EmptyDirHook) Process(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {

	podInfo, err := h.kClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		klog.Error(err, "[emptyStorage] Failed to get pod info", "pod", pod.Name, "namespace", pod.Namespace)
	}

	foundEmptyVolume := false

	for _, volume := range podInfo.Spec.Volumes {
		if volume.EmptyDir != nil && volume.EmptyDir.SizeLimit != nil && volume.EmptyDir.Medium != v1.StorageMediumMemory {
			klog.Infof("[emptyStorage] Detected emptyDir volume: %s with size limit: %d bytes",
				volume.Name, volume.EmptyDir.SizeLimit.Value())
			foundEmptyVolume = true
			break
		}
	}

	if !foundEmptyVolume {
		klog.Errorf("[emptyStorage] %s pod not have empty dir ,skipping ", pod.Name)
		return nil
	}

	for _, m := range container.Mounts {
		if strings.Contains(m.Source, "kubernetes.io~empty-dir") {
			parts := strings.Split(m.Source, "/")
			volumeName := parts[len(parts)-1]

			klog.Infof("[emptyStorage] Detected container %s mounting emptyDir: %s at physical path: %s",
				container.Name, volumeName, m.Source)

			limitBytes := uint64(0)
			for _, volume := range podInfo.Spec.Volumes {

				if volume.EmptyDir.Medium == v1.StorageMediumMemory {
					klog.Infof("[emptyStorage] emptyDir volume: %s is using memory medium, skipping quota setup", volume.Name)
					continue
				}

				if volume.EmptyDir.SizeLimit == nil {
					klog.Infof("[emptyStorage] emptyDir volume: %s does not have a size limit, skipping quota setup", volume.Name)
					continue
				}

				// Check if the volume name matches the current mount point

				if volume.Name == volumeName {
					limitBytes = uint64(volume.EmptyDir.SizeLimit.Value())
					break
				}
			}

			if limitBytes == 0 {
				continue
			}

			projectID, err := utils.GetProjectID()
			if err != nil {
				klog.ErrorS(err, "[emptyStorage] Failed to get project ID for emptyDir quota")
				return nil
			}

			if err = terminus_quota.SetProjectIDRecursive(m.Source, projectID); err != nil {
				klog.ErrorS(err, "[emptyStorage] Failed to set project ID for emptyDir quota", "path", m.Source, "projectID", projectID)
				return nil
			}

			if err = terminus_quota.SetQuota(m.Source, uint32(projectID), terminus_quota.ProjQuota, limitBytes/KB, 0, 0, 0); err != nil {
				klog.ErrorS(err, "[emptyStorage] Failed to set quota for emptyDir", "path", m.Source, "projectID", projectID, "limitBytes", limitBytes)
				return nil
			}

			h.store.TriggerUpdate(uint32(projectID), metadata.ContainerInfo{
				ProjectID:     uint32(projectID),
				Namespace:     pod.GetNamespace(),
				PodName:       pod.GetName(),
				ContainerName: container.GetName(),
				VolumeName:    volumeName,
				StorageType:   metadata.EMPTYDIR_TYPE,
			})

			if err := h.handleUpdatePod(ctx, pod.Name, pod.Namespace, volumeName, fmt.Sprintf("%d", uint32(projectID))); err != nil {
				klog.Warningf("[emptyStorage]  %s/%s pod label update failed, It may affect the reporting of pod disk monitoring metrics, err: %v",
					pod.Namespace, pod.Name, err)
			}

			klog.Infof("[emptyStorage] Successfully set quota for emptyDir: %s, projectID: %d, limitBytes: %d",
				m.Source, projectID, limitBytes)
		}
	}

	return nil
}

func (h *EmptyDirHook) Start(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	return nil
}

func (h *EmptyDirHook) Stop(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {

	podInfo, err := h.kClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		klog.Error(err, "[emptyStorage] Failed to get pod info", "pod", pod.Name, "namespace", pod.Namespace)
	}

	value, ok := podInfo.Labels[EmptyDirQuotaLabel]
	if !ok || value != "enabled" {
		klog.Infof("[emptyStorage] Pod %s/%s does not have emptyDir quota enabled, skipping cleanup", pod.Namespace, pod.Name)
		return nil
	}

	foundEmptyVolume := false

	for _, volume := range podInfo.Spec.Volumes {
		if volume.EmptyDir != nil && volume.EmptyDir.SizeLimit != nil {
			klog.Infof("[emptyStorage] Detected emptyDir volume: %s with size limit: %d bytes",
				volume.Name, volume.EmptyDir.SizeLimit.Value())
			foundEmptyVolume = true
			break
		}
	}

	if !foundEmptyVolume {
		klog.Errorf("[emptyStorage] %s pod not have empty dir ,skipping ", pod.Name)
		return nil
	}

	for _, m := range container.Mounts {
		if strings.Contains(m.Source, "kubernetes.io~empty-dir") {
			parts := strings.Split(m.Source, "/")
			volumeName := parts[len(parts)-1]

			klog.Infof("[emptyStorage] Detected container %s mounting emptyDir: %s at physical path: %s",
				container.Name, volumeName, m.Source)

			projectID, err := strconv.ParseUint(
				podInfo.Annotations[fmt.Sprintf("%s.%s", EmptyDirPrjIDAnnotation, volumeName)], 10, 32)
			if err != nil {
				klog.Errorf("[emptyStorage] transform emptydir project id failed : %v\n %s\n", err, fmt.Sprintf("%s.%s", EmptyDirPrjIDAnnotation, container.Name))
				return nil
			}

			if err := terminus_quota.RemoveQuota(h.kubeleRootPath, uint32(projectID), terminus_quota.ProjQuota); err != nil {
				klog.Warningf("[emptyStorage] remove Project ID quota for %s, failed", m.Source)
				return err
			}

			if err = terminus_quota.ClearProjectID(h.kubeleRootPath); err != nil {
				klog.Warningf("[emptyStorage] clear Project ID for %s, failed", m.Source)
			}

			h.store.TriggerDelete(uint32(projectID))

			klog.Infof("[emptyStorage] Successfully remove quota for emptyDir: %s, projectID: %d",
				m.Source, projectID)
		}
	}

	return nil
}

func (h *EmptyDirHook) handleUpdatePod(ctx context.Context, podName, namespace, volumeName, projectID string) error {
	containerAnnotation := fmt.Sprintf("%s.%s", EmptyDirPrjIDAnnotation, volumeName)
	patchPayload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				EmptyDirQuotaLabel: "enabled",
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
