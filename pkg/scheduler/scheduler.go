package scheduler

import (
	"context"
	"fmt"
	"sync"

	"github.com/terminus-io/Terminus/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	schdulerFramework "k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
)

const (
	SchedulerName       = "terminus-scheduler"
	nodeAnnotationTotal = "storage.terminus.io/physical-total" // NRI 插件上报的 Key
	nodeAnnotationUsed  = "storage.terminus.io/physical-used"
	threshold           = 0.95
)

type TerminusSchedulerPlugin struct {
	handle     schdulerFramework.Handle
	statsCache sync.Map
	podLister  listersv1.PodLister
	args       *TerminusArgs
}

var _ schdulerFramework.FilterPlugin = &TerminusSchedulerPlugin{}
var _ schdulerFramework.ScorePlugin = &TerminusSchedulerPlugin{}

func New(ctx context.Context, obj runtime.Object, h schdulerFramework.Handle) (schdulerFramework.Plugin, error) {

	args := &TerminusArgs{}
	args.SetDefaults()
	if err := frameworkruntime.DecodeInto(obj, args); err != nil {
		return nil, fmt.Errorf("failed to decode TerminusArgs: %v", err)
	}

	if args.OversubscriptionRatio < 1.0 {
		return nil, fmt.Errorf("oversubscriptionRatio must be >= 1.0, got %f", args.OversubscriptionRatio)
	}

	klog.V(4).Infof("Terminus Scheduler loaded with Ratio: %.2f\n", args.OversubscriptionRatio)

	podLister := h.SharedInformerFactory().Core().V1().Pods().Lister()
	plugin := &TerminusSchedulerPlugin{
		handle:    h,
		podLister: podLister,
		args:      args,
	}
	nodeInformer := h.SharedInformerFactory().Core().V1().Nodes().Informer()

	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    plugin.handleNodeUpdate,
		UpdateFunc: func(oldObj, newObj interface{}) { plugin.handleNodeUpdate(newObj) },
		DeleteFunc: plugin.handleNodeDelete,
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

	_, totalanno := node.Annotations[nodeAnnotationTotal]
	_, useanno := node.Annotations[nodeAnnotationUsed]

	if !totalanno || !useanno {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable,
			fmt.Sprintf("%s not have annotation , matbe not open quota feaure, please check , skip this....", node.Name))
	}

	requestBytes := utils.GetPodTotalStorage(pod)
	val, ok := p.statsCache.Load(node.Name)
	if !ok {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable, "Node storage stats missing")
	}
	stats := val.(map[string]int64)

	//计算剩余空间 (支持超卖)
	capacity := stats[nodeAnnotationTotal]
	free := capacity - stats[nodeAnnotationUsed]
	overCommit := int64(float64(capacity) * p.args.OversubscriptionRatio)
	var nodeExistingAllocated int64 = 0

	for _, podInfo := range nodeInfo.Pods {
		nodeExistingAllocated += utils.GetPodTotalStorage(podInfo.Pod)
	}

	if (nodeExistingAllocated + requestBytes) >= overCommit {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable,
			fmt.Sprintf("Insufficient  storage: req %d, free %d", requestBytes, overCommit-nodeExistingAllocated))
	}

	safeLimit := int64(float64(capacity) * threshold)

	if stats[nodeAnnotationUsed] > safeLimit {
		return schdulerFramework.NewStatus(schdulerFramework.Unschedulable,
			fmt.Sprintf("Insufficient Physical storage: used %d > limit %d (95%%)",
				requestBytes, free))
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
	overCommit := int64(float64(capacity) * p.args.OversubscriptionRatio)
	podRequest := utils.GetPodTotalStorage(pod)

	var existingAllocated int64 = 0

	for _, podInfo := range nodeInfo.Pods {
		existingAllocated += utils.GetPodTotalStorage(podInfo.Pod)
	}

	logicalFree := overCommit - (existingAllocated + podRequest)

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
