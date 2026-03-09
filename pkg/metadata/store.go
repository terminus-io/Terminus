package metadata

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type EventType int

const (
	EventUpdate EventType = iota
	EventDelete
	quotaEnableLabel        = "storage.terminus.io/quota"
	projectIDAnnotation     = "storage.terminus.io/project-id"
	emptyDirPrjIDAnnotation = "emptydir.terminus.io/project-id"
	emptyDirQuotaLabel      = "emptydir.terminus.io/quota"
)

type UpdateEvent struct {
	Type      EventType
	ProjectID uint32
	Info      ContainerInfo
}

type AsyncStore struct {
	data     map[uint32]ContainerInfo
	mu       sync.RWMutex
	updateCh chan UpdateEvent
	kClient  kubernetes.Interface
}

func NewAsyncStore(bufferSize int, kclient kubernetes.Interface) *AsyncStore {
	return &AsyncStore{
		data:     make(map[uint32]ContainerInfo),
		updateCh: make(chan UpdateEvent, bufferSize),
		kClient:  kclient,
	}
}

func (s *AsyncStore) TriggerUpdate(id uint32, info ContainerInfo) {
	select {
	case s.updateCh <- UpdateEvent{Type: EventUpdate, ProjectID: id, Info: info}:
	default:
		klog.ErrorS(nil, "Metadata update channel full, dropping event", "id", id)
	}
}

func (s *AsyncStore) TriggerDelete(id uint32) {
	select {
	case s.updateCh <- UpdateEvent{Type: EventDelete, ProjectID: id}:
	default:
		klog.ErrorS(nil, "Metadata update channel full, dropping delete", "id", id)
	}
}

func (s *AsyncStore) Run(ctx context.Context) {
	klog.Info("Async metadata store worker started")

	for {
		select {
		case <-ctx.Done():
			klog.Info("Async store worker stopped")
			return
		case event := <-s.updateCh:
			s.handleEvent(event)
		}
	}
}

func (s *AsyncStore) handleEvent(e UpdateEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch e.Type {
	case EventUpdate:
		s.data[e.ProjectID] = e.Info
		klog.V(4).InfoS("Async updated metadata", "id", e.ProjectID)

	case EventDelete:
		delete(s.data, e.ProjectID)
		klog.V(4).InfoS("Async deleted metadata", "id", e.ProjectID)
	}
}

func (s *AsyncStore) Get(id uint32) (ContainerInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[id]
	return val, ok
}

func (s *AsyncStore) TriggerRestore() {
	nodeName := os.Getenv("NODE_NAME")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	klog.InfoS("[Restore Metrics] Start List Pods", "node", nodeName, "label", quotaEnableLabel)

	pods, err := s.kClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})

	if err != nil {
		klog.Errorf("[Restore Metrics] List Pods failed, monitoring metrics of existing pods may be affected: %v\n", err)
		return
	}

	var matchedPods []corev1.Pod

	for _, pod := range pods.Items {
		// 情况 A：如果你要求标签的值必须是 "enabled"（和截图里 %s=enabled 一致）
		if pod.Labels[quotaEnableLabel] == "enabled" || pod.Labels[emptyDirQuotaLabel] == "enabled" {
			matchedPods = append(matchedPods, pod)
			continue
		}
	}

	pods.Items = matchedPods

	prefix := projectIDAnnotation + "."
	for _, pod := range pods.Items {
		for key, val := range pod.Annotations {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			containerName := strings.TrimPrefix(key, prefix)
			projectID, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				continue
			}

			klog.V(4).Infof("[Restore Metrics] Target detected: [%s/%s] Container: %s -> ProjectID: %d; Start Restore this\n",
				pod.Namespace, pod.Name, containerName, projectID)

			s.TriggerUpdate(uint32(projectID), ContainerInfo{
				ProjectID:     uint32(projectID),
				Namespace:     pod.Namespace,
				PodName:       pod.Name,
				ContainerName: containerName,
				StorageType:   ROOTFS_TYPE,
			})
		}
	}

	klog.Infof("[Restore Metrics] Node:%s container rootfs info metrics all restore", nodeName)

	emptyPrefix := emptyDirPrjIDAnnotation + "."
	for _, pod := range pods.Items {
		for key, val := range pod.Annotations {
			if !strings.HasPrefix(key, emptyPrefix) {
				continue
			}
			volumeName := strings.TrimPrefix(key, emptyPrefix)
			projectID, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				continue
			}

			containerName := ""
			for _, container := range pod.Spec.Containers {
				for _, volume := range container.VolumeMounts {
					if volume.Name == volumeName {
						containerName = container.Name
						break
					}
				}

			}

			klog.V(4).Infof("[EmptyStorage Restore Metrics] Target detected: [%s/%s] Container: %s -> ProjectID: %d -> Volume: %s; Start Restore this\n",
				pod.Namespace, pod.Name, containerName, projectID, volumeName)

			s.TriggerUpdate(uint32(projectID), ContainerInfo{
				ProjectID:     uint32(projectID),
				Namespace:     pod.Namespace,
				PodName:       pod.Name,
				ContainerName: containerName,
				VolumeName:    volumeName,
				StorageType:   EMPTYDIR_TYPE,
			})
		}
	}

	klog.Infof("[EmptyStorage Restore Metrics] Node:%s container emptyStorage info metrics all restore", nodeName)
}
