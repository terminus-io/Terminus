package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// 定义上报的数据结构 (会变成 Annotation 的 Value)
type NodeStorageStats struct {
	TotalBytes      uint64  `json:"total"`
	OvercommitRatio float64 `json:"ratio"` // 超卖比
}

const (
	NodeAnnotationKey = "storage.terminus.io/stats"
	nodeResourceName  = "storage.terminus.io/size"
)

func (r *reporter) ReportToAnnotation(ctx context.Context, diskUsage uint64) error {
	stats := NodeStorageStats{
		OvercommitRatio: 1, // 允许 20% 超卖
		TotalBytes:      diskUsage,
	}

	// 2. 序列化 Value (JSON String)
	statsBytes, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	statsStr := string(statsBytes)

	// 3. 构造 Patch Payload
	// 格式: {"metadata": {"annotations": {"key": "value"}}}
	patchMap := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				NodeAnnotationKey: statsStr,
			},
		},
	}

	patchData, err := json.Marshal(patchMap)
	if err != nil {
		return err
	}

	_, err = r.kClient.CoreV1().Nodes().Patch(
		ctx,
		os.Getenv("NODE_NAME"),
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{},
	)

	if err != nil {
		return fmt.Errorf("failed to patch node annotation: %w", err)
	}

	klog.V(4).InfoS("Updated node stats annotation", "node", os.Getenv("NODE_NAME"))

	virtualCapacity := uint64(float64(stats.TotalBytes) * stats.OvercommitRatio)
	capacityQty := resource.NewQuantity(int64(virtualCapacity), resource.BinarySI)

	statusPatch := map[string]interface{}{
		"status": map[string]interface{}{
			"capacity": map[string]string{
				nodeResourceName: capacityQty.String(),
			},
			"allocatable": map[string]string{
				nodeResourceName: capacityQty.String(),
			},
		},
	}

	statusJson, _ := json.Marshal(statusPatch)

	_, err = r.kClient.CoreV1().Nodes().Patch(
		ctx,
		os.Getenv("NODE_NAME"),
		types.MergePatchType,
		statusJson,
		metav1.PatchOptions{},
		"status",
	)

	if err != nil {
		return fmt.Errorf("failed to patch node resource status: %w", err)
	}

	klog.V(4).InfoS("Updated node resource status", "node", os.Getenv("NODE_NAME"))

	return nil
}
