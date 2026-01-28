package nri

import (
	"context"

	"github.com/containerd/nri/pkg/api"
	"k8s.io/klog/v2"
)

func (e *Enforcer) CreateContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	klog.V(2).InfoS("Event: CreateContainer", "container", container.Name)

	// --- 循环执行所有 Hook ---
	for _, hook := range e.Hooks {
		if err := hook.Process(ctx, pod, container); err != nil {
			klog.ErrorS(err, "Hook failed", "hook", hook.Name())
			return nil, nil, err
		}
	}

	return nil, nil, nil
}

func (e *Enforcer) StartContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	klog.V(2).InfoS("Event: StartContainer", "container", container.Name)

	// --- 循环执行所有 Hook ---
	for _, hook := range e.Hooks {
		if err := hook.Start(ctx, pod, container); err != nil {
			klog.ErrorS(err, "Hook failed", "hook", hook.Name())
			return err
		}
	}
	return nil
}

func (e *Enforcer) StopContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) ([]*api.ContainerUpdate, error) {
	klog.V(2).InfoS("Event: StopContainer", "container", container.Name)

	// --- 循环执行所有 Hook ---
	for _, hook := range e.Hooks {
		if err := hook.Stop(ctx, pod, container); err != nil {
			klog.ErrorS(err, "Hook failed", "hook", hook.Name())
			return nil, err
		}
	}
	return nil, nil
}
