package scheduler

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type TerminusArgs struct {
	metav1.TypeMeta       `json:",inline"`
	Namespace             string  `json:"namespace"`
	OversubscriptionRatio float64 `json:"oversubscriptionRatio"`
	UseAI                 bool    `json:"useAI"`
	AiWeightRatio         int     `json:"aiWeightRatio"`
	ModelType             string  `json:"modelType"`
	ModelName             string  `json:"modelName"`
	OpenAIAPIKey          string  `json:"openAIAPIKey"`
	OpenAIAPIURL          string  `json:"openAIAPIURL"`
}

// 默认配置
func (args *TerminusArgs) SetDefaults() {
	if args.OversubscriptionRatio == 0 {
		args.OversubscriptionRatio = 1.0
	}

	if args.AiWeightRatio == 0 {
		args.AiWeightRatio = 30
	}

	if args.AiWeightRatio > 100 || args.AiWeightRatio < 0 {
		klog.Warningf("Invalid AiWeightRatio: %d. Must be between 0 and 100. Reverting to default (40).", args.AiWeightRatio)
		args.AiWeightRatio = 30
	}

	if args.Namespace == "" {
		args.Namespace = "kube-system"
	}
}
