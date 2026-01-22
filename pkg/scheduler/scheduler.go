package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// 插件名称
const (
	schedulerName   = "terminus-scheduler"
	annotationKey   = "storage.terminus.io/stats" // NRI 插件上报的 Key
	limitAnnotation = "storage.terminus.io/size"  // Pod 申请的大小
)

// NodeStorageStats 对应 Node Annotation 中的 JSON 结构
type nodeStorageStats struct {
	TotalBytes      uint64  `json:"total"`
	OvercommitRatio float64 `json:"ratio"`
	AllocatedBytes  uint64
}

// TerminusPlugin 插件结构体
type TerminusSchedulerPlugin struct {
	handle     framework.Handle
	statsCache sync.Map
}

// 确保实现了必要的接口
var _ framework.FilterPlugin = &TerminusSchedulerPlugin{}
var _ framework.ScorePlugin = &TerminusSchedulerPlugin{}

func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	plugin := &TerminusSchedulerPlugin{
		handle: h,
	}

	// =======================================================
	// 1. 设置 Informer 监听
	// =======================================================
	// SharedInformerFactory 是框架自带的，复用连接，效率极高
	nodeInformer := h.SharedInformerFactory().Core().V1().Nodes().Informer()

	// 注册回调：当 Node 发生变化时触发
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    plugin.handleNodeUpdate,                                              // 新增节点
		UpdateFunc: func(oldObj, newObj interface{}) { plugin.handleNodeUpdate(newObj) }, // 更新节点
		DeleteFunc: plugin.handleNodeDelete,                                              // 删除节点
	})

	return plugin, nil
}

func (p *TerminusSchedulerPlugin) Name() string { return schedulerName }

func (p *TerminusSchedulerPlugin) handleNodeUpdate(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}

	// 1. 获取 Annotation
	val := node.Annotations[annotationKey]
	if val == "" {
		p.statsCache.Delete(node.Name)
		return
	}

	// 2. 解析 JSON (这里稍微耗时，但只在节点更新时做一次)
	var stats nodeStorageStats
	if err := json.Unmarshal([]byte(val), &stats); err != nil {
		klog.ErrorS(err, "Failed to parse terminus stats", "node", node.Name)
		return
	}

	// 3. 存入极速缓存
	p.statsCache.Store(node.Name, &stats)
}

func (p *TerminusSchedulerPlugin) handleNodeDelete(obj interface{}) {
	if node, ok := obj.(*v1.Node); ok {
		p.statsCache.Delete(node.Name)
	}
}

func (p *TerminusSchedulerPlugin) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node not found")
	}
	// 实际项目中建议使用 resource.ParseQuantity
	requestStr := pod.Annotations[limitAnnotation]
	if requestStr == "" {
		return nil // 不需要存储，直接放行
	}
	// 假设这里解析出来了 10GB (这里简化写死，实际需解析 requestStr)
	var requestBytes uint64 = 10 * 1024 * 1024 * 1024

	// 2. 【极速】从缓存读取节点状态
	val, ok := p.statsCache.Load(node.Name)
	if !ok {
		// 节点没有上报数据，视为不可调度 (或者你可以策略性放行)
		return framework.NewStatus(framework.Unschedulable, "Node storage stats missing")
	}
	stats := val.(*nodeStorageStats)

	// 3. 计算剩余空间 (支持超卖)
	capacity := float64(stats.TotalBytes) * stats.OvercommitRatio
	free := int64(capacity) - int64(stats.AllocatedBytes)

	if free < int64(requestBytes) {
		return framework.NewStatus(framework.Unschedulable,
			fmt.Sprintf("Insufficient storage: req %d, free %d", requestBytes, free))
	}

	return nil
}

// Score: 剩余空间越大的节点，分数越高 (LeastAllocated 策略)
func (p *TerminusSchedulerPlugin) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	val, ok := p.statsCache.Load(nodeName)
	if !ok {
		return 0, nil
	}
	stats := val.(*nodeStorageStats)

	// 计算剩余率
	capacity := float64(stats.TotalBytes) * stats.OvercommitRatio
	free := int64(capacity) - int64(stats.AllocatedBytes)

	if free <= 0 {
		return 0, nil
	}

	// 归一化为 0-100 分
	// 分数 = (剩余空间 / 总空间) * 100
	score := int64((float64(free) / capacity) * float64(framework.MaxNodeScore))

	return score, nil
}

func (p *TerminusSchedulerPlugin) ScoreExtensions() framework.ScoreExtensions { return nil }
