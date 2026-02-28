package scheduler

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/terminus-io/Terminus/pkg/utils"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

const (
	GB               = 1024 * 1024 * 1024
	NexusInterval    = 30 * time.Second
	MaxNodesInPrompt = 50
)

func setupNexusAnalyzer(args *TerminusArgs) (blades.Agent, error) {
	if args.UseAI && (args.ModelType == "" || args.ModelName == "" || args.OpenAIAPIKey == "" || args.OpenAIAPIURL == "") {
		return nil, fmt.Errorf("UseAI is true, but ModelType, ModelName, OpenAIAPIKey are empty")
	}

	model := openai.NewModel(args.ModelName, openai.Config{
		APIKey:  args.OpenAIAPIKey,
		BaseURL: args.OpenAIAPIURL,
	})

	agent, err := blades.NewAgent(
		"nexus-scheduler",
		blades.WithModel(model),
		blades.WithInstruction(nexusPromptUseAI),
	)
	if err != nil {
		klog.Errorf("Failed to create  agent: %v", err)
		return nil, err
	}

	return agent, nil
}

func (p *TerminusSchedulerPlugin) runNexusAnalyzer(ctx context.Context, agent blades.Agent) {

	ticker := time.NewTicker(NexusInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			klog.Info("Nexus Analyzer stopped due to context cancellation.")
			return
		case <-ticker.C:
			// Continue execution
		}

		lease, err := p.handle.ClientSet().CoordinationV1().Leases(p.args.Namespace).Get(context.TODO(), SchedulerName, metav1.GetOptions{})
		if err != nil {
			klog.Warningf("Failed to get Leader Lease, postponing AI polling: %v", err)
			continue
		}

		holder := ""
		if lease.Spec.HolderIdentity != nil {
			holder = *lease.Spec.HolderIdentity
		}

		if !strings.HasPrefix(holder, os.Getenv("HOSTNAME")) {
			klog.V(5).Infof("[Nexus Hibernation] Current Leader is %s, I am a standby, skipping AI inference to save resources.", holder)
			continue
		}

		// Use SharedInformerFactory for efficient and persistent data access
		nodeLister := p.handle.SharedInformerFactory().Core().V1().Nodes().Lister()
		podLister := p.handle.SharedInformerFactory().Core().V1().Pods().Lister()

		nodes, err := nodeLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("Failed to list Nodes: %v", err)
			continue
		}

		pods, err := podLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("Failed to list Pods: %v", err)
			continue
		}

		// Pre-aggregate Pod usage per Node to avoid O(N*M)
		nodePodUsage := make(map[string]int64)
		for _, pod := range pods {
			if pod.Spec.NodeName != "" && pod.Status.Phase != "Succeeded" && pod.Status.Phase != "Failed" {
				nodePodUsage[pod.Spec.NodeName] += utils.GetPodTotalStorage(pod)
			}
		}

		var nodeRows []string

		for _, node := range nodes {
			if node == nil {
				continue
			}

			physicalTotal, exists := node.Annotations[nodeAnnotationTotal]
			if !exists {
				klog.V(5).Infof("Node %s annotation %s is nil", node.Name, nodeAnnotationTotal)
				continue
			}

			physicalUsed, exists := node.Annotations[nodeAnnotationUsed]
			if !exists {
				klog.V(5).Infof("Node %s annotation %s is nil", node.Name, nodeAnnotationUsed)
				continue
			}

			usedQty, err := resource.ParseQuantity(physicalUsed)
			if err != nil {
				klog.V(5).Infof("Node %s annotation %s parse quantity failed: %v", node.Name, nodeAnnotationUsed, err)
				continue
			}
			totalQty, err := resource.ParseQuantity(physicalTotal)
			if err != nil {
				klog.V(5).Infof("Node %s annotation %s parse quantity failed: %v", node.Name, nodeAnnotationTotal, err)
				continue
			}

			if totalQty.Value() == 0 {
				continue
			}

			existingAllocated := nodePodUsage[node.Name]
			usageRate := (usedQty.Value() * 100) / totalQty.Value()
			totalQuota := int64(float64(totalQty.Value())*p.args.OversubscriptionRatio) / GB
			existingAllocatedGB := existingAllocated / GB

			row := fmt.Sprintf("| %s | %s | %s | %d%% | %dGB | %dGB |",
				node.Name, physicalTotal, physicalUsed, usageRate, existingAllocatedGB, totalQuota)
			nodeRows = append(nodeRows, row)
		}

		if len(nodeRows) == 0 {
			klog.V(4).Info("No nodes with Terminus annotations found, skipping AI analysis")
			continue
		}

		// Process in batches
		allScores := make(map[string]int64)

		for i := 0; i < len(nodeRows); i += MaxNodesInPrompt {
			end := i + MaxNodesInPrompt
			if end > len(nodeRows) {
				end = len(nodeRows)
			}
			batchRows := nodeRows[i:end]

			var promptBuilder strings.Builder
			promptBuilder.WriteString("The storage status of each node in the current Kubernetes cluster is as follows:\n")
			promptBuilder.WriteString("| Node Name | Disk Size | Used | Disk Usage | Quota Use | Total Quota |\n")
			promptBuilder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
			for _, row := range batchRows {
				promptBuilder.WriteString(row + "\n")
			}

			klog.V(5).Infof("Nexus Analyzer Batch Prompt (%d-%d): %s", i, end, promptBuilder.String())
			userMsg := fmt.Sprintf("Here is the node storage snapshot of the current cluster, please immediately score the risk according to the rules:\n%s", promptBuilder.String())

			input := blades.UserMessage(userMsg)
			runner := blades.NewRunner(agent)
			output, err := runner.Run(ctx, input)
			if err != nil {
				klog.Errorf("Failed to run agent for batch %d-%d: %v", i, end, err)
				continue // Skip this batch but try others
			}

			klog.V(4).Infof("Nexus Analyzer Batch Response: %s", output.Text())

			parsedScores, err := parseLLMOutput(output.Text())
			if err != nil {
				klog.Errorf("Failed to parse LLM output for batch %d-%d: %v", i, end, err)
				continue
			}

			// Merge scores
			for nodeName, score := range parsedScores {
				allScores[nodeName] = score
			}
		}

		p.scoreLock.Lock()
		p.aiScores = allScores
		p.scoreLock.Unlock()
		klog.Infof("Nexus Analyzer Updated Scores for %d nodes", len(allScores))
	}
}
