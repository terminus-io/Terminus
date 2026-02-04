package utils

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	KeyGlobalDefault   = "storage.terminus.io/size"
	PrefixSpecific     = "storage.terminus.io/size."
	DefaultMainSize    = "2Gi"
	DefaultSidecarSize = "500Mi"
)

func GetPodTotalStorage(pod *v1.Pod) int64 {
	var total int64 = 0

	for _, c := range pod.Spec.Containers {
		total += GetContainerQuota(pod.Annotations, c.Name)
	}
	for _, c := range pod.Spec.InitContainers {
		total += GetContainerQuota(pod.Annotations, c.Name)
	}

	return total
}

func GetContainerQuota(annotations map[string]string, containerName string) int64 {

	if val, ok := annotations[PrefixSpecific+containerName]; ok {
		return parseSize(val)
	}

	if val, ok := annotations[KeyGlobalDefault]; ok {
		return parseSize(val)
	}

	if IsSidecar(containerName) {
		return parseSize(DefaultSidecarSize)
	}

	return parseSize(DefaultMainSize)
}

func parseSize(q string) int64 {
	qty, err := resource.ParseQuantity(q)
	if err != nil {
		return 0
	}
	return qty.Value()
}

func IsSidecar(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "sidecar") ||
		strings.Contains(name, "agent") ||
		strings.Contains(name, "proxy") ||
		strings.Contains(name, "filebeat")
}
