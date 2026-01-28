package scheduler

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	schdulerFramework "k8s.io/kubernetes/pkg/scheduler/framework"
)

// 插件名称
const (
	SchedulerName       = "terminus-scheduler"
	nodeAnnotationTotal = "storage.terminus.io/physical-total" // NRI 插件上报的 Key
	nodeAnnotationUsed  = "storage.terminus.io/physical-used"
	podLimitAnnotation  = "storage.terminus.io/size" // Pod 申请的大小
	threshold           = 0.95
)

// TerminusPlugin 插件结构体
type TerminusSchedulerPlugin struct {
	handle     schdulerFramework.Handle
	statsCache sync.Map
	podLister  listersv1.PodLister
}

// 确保实现了必要的接口
var _ schdulerFramework.FilterPlugin = &TerminusSchedulerPlugin{}
var _ schdulerFramework.ScorePlugin = &TerminusSchedulerPlugin{}

func New(ctx context.Context, _ runtime.Object, h schdulerFramework.Handle) (schdulerFramework.Plugin, error) {
	podLister := h.SharedInformerFactory().Core().V1().Pods().Lister()
	plugin := &TerminusSchedulerPlugin{
		handle:    h,
		podLister: podLister,
	}
	nodeInformer := h.SharedInformerFactory().Core().V1().Nodes().Informer()

	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    plugin.handleNodeUpdate,                                              // 新增节点
		UpdateFunc: func(oldObj, newObj interface{}) { plugin.handleNodeUpdate(newObj) }, // 更新节点
		DeleteFunc: plugin.handleNodeDelete,                                              // 删除节点
	})

	return plugin, nil
}

func (p *TerminusSchedulerPlugin) Name() string { return SchedulerName }

func (p *TerminusSchedulerPlugin) handleNodeUpdate(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}

	totalAnno, err := resource.ParseQuantity(node.Annotations[nodeAnnotationTotal])
	if err != nil {
		return
	}

	usedAnno, err := resource.ParseQuantity(node.Annotations[nodeAnnotationUsed])

	if err != nil {
		return
	}

	if totalAnno.String() == "" || usedAnno.String() == "" {
		p.statsCache.Delete(node.Name)
		return
	}

	storageInfo := map[string]int64{nodeAnnotationUsed: usedAnno.Value(), nodeAnnotationTotal: totalAnno.Value()}
	p.statsCache.Store(node.Name, storageInfo)
}

func (p *TerminusSchedulerPlugin) handleNodeDelete(obj interface{}) {
	if node, ok := obj.(*v1.Node); ok {
		p.statsCache.Delete(node.Name)
	}
}

func (p *TerminusSchedulerPlugin) Filter(ctx context.Context, state *schdulerFramework.CycleState, pod *v1.Pod, nodeInfo *schdulerFramework.NodeInfo) *schdulerFramework.Status {
	node := nodeInfo.Node()

	if node == nil {
		return schdulerFramework.NewStatus(schdulerFramework.Error, "node not found")
	}
	requestStr := pod.Annotations[podLimitAnnotation]
	if requestStr == "" {
		return nil
	}

	requestBytes, err := resource.ParseQuantity(requestStr)
	if err != nil {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable,
			fmt.Sprintf("Invalid Parse : %s ", requestStr))
	}

	val, ok := p.statsCache.Load(node.Name)
	if !ok {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable, "Node storage stats missing")
	}
	stats := val.(map[string]int64)

	// 3. 计算剩余空间 (支持超卖)
	capacity := stats[nodeAnnotationTotal]
	free := capacity - stats[nodeAnnotationUsed]
	overCommit := int64(float64(capacity) * 1.2)

	var nodeExistingAllocated int64 = 0
	for _, podInfo := range nodeInfo.Pods {
		if str, ok := podInfo.Pod.Annotations[podLimitAnnotation]; ok {
			if q, err := resource.ParseQuantity(str); err == nil {
				nodeExistingAllocated += q.Value()
			}
		}
	}

	if (nodeExistingAllocated + requestBytes.Value()) >= overCommit {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable,
			fmt.Sprintf("Insufficient  storage: req %d, free %d", requestBytes.Value(), overCommit-nodeExistingAllocated))
	}

	safeLimit := int64(float64(capacity) * threshold)

	if stats[nodeAnnotationUsed] > safeLimit {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable,
			fmt.Sprintf("Insufficient Physical storage: used %d > limit %d (95%%)",
				requestBytes.Value(), free))
	}

	klog.V(4).Infof("%s pod schedule node %s ", pod.Name, node.Name)
	return nil
}

// Score: 剩余空间越大的节点，分数越高 (LeastAllocated 策略)
func (p *TerminusSchedulerPlugin) Score(ctx context.Context, state *schdulerFramework.CycleState, pod *v1.Pod, nodeName string) (int64, *schdulerFramework.Status) {
	nodeInfo, err := p.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, nil
	}
	val, ok := p.statsCache.Load(nodeInfo.Node().Name)
	if !ok {
		return 0, nil
	}

	stats := val.(map[string]int64)
	capacity := stats[nodeAnnotationTotal]
	free := capacity - stats[nodeAnnotationUsed]
	overCommit := int64(float64(capacity) * 1.2)

	var existingAllocated int64 = 0
	for _, podInfo := range nodeInfo.Pods {
		if str, ok := podInfo.Pod.Annotations[podLimitAnnotation]; ok {
			if q, err := resource.ParseQuantity(str); err == nil {
				existingAllocated += q.Value()
			}
		}
	}

	logicalFree := overCommit - existingAllocated

	if logicalFree <= 0 || free <= 0 {
		return 0, nil
	}
	logicalScore := int64((float64(logicalFree) / float64(overCommit)) * float64(schdulerFramework.MaxNodeScore))
	physicalScore := int64((float64(free) / float64(capacity)) * float64(schdulerFramework.MaxNodeScore))

	score := min(logicalScore, physicalScore)
	klog.V(4).Infof("%s pod, node %s score is : %v ", pod.Name, nodeName, score)

	return score, nil
}

func (p *TerminusSchedulerPlugin) ScoreExtensions() schdulerFramework.ScoreExtensions { return nil }
