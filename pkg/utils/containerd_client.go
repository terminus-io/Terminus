package utils

import (
	"context"
	"sync"

	"github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/snapshots"
	"k8s.io/klog/v2"
)

type ContainerdClientWrapper struct {
	socketPath string
	namespace  string
	client     *client.Client
	mu         sync.RWMutex
}

func NewContainerdClientWrapper(socketPath, namespace string) *ContainerdClientWrapper {
	return &ContainerdClientWrapper{
		socketPath: socketPath,
		namespace:  namespace,
	}
}

func (w *ContainerdClientWrapper) connect() (*client.Client, error) {
	return client.New(w.socketPath)
}

func (w *ContainerdClientWrapper) getClient(ctx context.Context) (*client.Client, error) {
	w.mu.RLock()
	c := w.client
	w.mu.RUnlock()

	if c != nil {
		return c, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.client != nil {
		return w.client, nil
	}

	c, err := w.connect()
	if err != nil {
		return nil, err
	}

	w.client = c
	klog.Info("Successfully connected to containerd")
	return c, nil
}

func (w *ContainerdClientWrapper) reconnect(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.client != nil {
		if err := w.client.Close(); err != nil {
			klog.Warningf("Failed to close old containerd client: %v", err)
		}
		w.client = nil
	}

	c, err := w.connect()
	if err != nil {
		return err
	}

	w.client = c
	klog.Info("Successfully reconnected to containerd")
	return nil
}

func (w *ContainerdClientWrapper) LoadContainer(ctx context.Context, id string) (client.Container, error) {
	c, err := w.getClient(ctx)
	if err != nil {
		return nil, err
	}

	cont, err := c.LoadContainer(ctx, id)
	if err != nil {
		klog.Warningf("Failed to load container %s, attempting to reconnect: %v", id, err)

		if reconnectErr := w.reconnect(ctx); reconnectErr != nil {
			return nil, err
		}

		c, err = w.getClient(ctx)
		if err != nil {
			return nil, err
		}

		return c.LoadContainer(ctx, id)
	}

	return cont, nil
}

func (w *ContainerdClientWrapper) SnapshotService(name string) snapshots.Snapshotter {
	c, err := w.getClient(context.Background())
	if err != nil {
		klog.Errorf("Failed to get containerd client for snapshot service: %v", err)
		return nil
	}

	return c.SnapshotService(name)
}

func (w *ContainerdClientWrapper) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.client != nil {
		return w.client.Close()
	}
	return nil
}
