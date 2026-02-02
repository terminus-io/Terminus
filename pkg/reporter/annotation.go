package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type NodeStorageStats struct {
	TotalBytes      uint64  `json:"total"`
	OvercommitRatio float64 `json:"ratio"` // 超卖比
}

const (
	nodeStoragePhyTotal = "storage.terminus.io/physical-total"
	nodeStoragePhyUsed  = "storage.terminus.io/physical-used"
	GiB                 = 1024 * 1024 * 1024
)

func (r *reporter) ReportToAnnotation(ctx context.Context, diskUsage, diskTotal uint64) error {

	usage := fmt.Sprintf("%vGi", diskUsage/1024/1024/1024)
	total := fmt.Sprintf("%vGi", diskTotal/1024/1024/1024)
	patchMap := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				nodeStoragePhyTotal: total,
				nodeStoragePhyUsed:  usage,
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

	statusPatch := map[string]interface{}{
		"status": map[string]interface{}{
			"capacity": map[string]string{
				nodeStoragePhyTotal: total,
			},
			"allocatable": map[string]string{
				nodeStoragePhyTotal: total,
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
