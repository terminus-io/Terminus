package nri

import (
	"context"
	"os"
	"time"

	"github.com/containerd/nri/pkg/stub"
	"k8s.io/klog/v2"
)

const (
	initialRetryDelay = 1 * time.Second
	maxRetryDelay     = 30 * time.Second
	maxRetries        = -1 // -1 表示无限重试
)

func NewEnforcer(opts ...Option) (*Enforcer, error) {

	e := &Enforcer{}
	for _, opt := range opts {
		opt(e)
	}
	return e, nil
}

func (e *Enforcer) Run(ctx context.Context) error {
	retryCount := 0
	retryDelay := initialRetryDelay

	for maxRetries == -1 || retryCount < maxRetries {
		if retryCount > 0 {
			klog.Infof("Attempting to reconnect to NRI (attempt %d)...", retryCount)
		}

		if _, err := os.Stat(e.SocketPath); os.IsNotExist(err) {
			klog.Warningf("NRI socket %s does not exist, waiting...", e.SocketPath)
			select {
			case <-time.After(retryDelay):
				retryDelay = calculateBackoff(retryDelay)
				retryCount++
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		opts := []stub.Option{
			stub.WithPluginName(e.PluginName),
			stub.WithPluginIdx(e.PluginIdx),
			stub.WithSocketPath(e.SocketPath),
		}
		st, err := stub.New(e, opts...)
		if err != nil {
			klog.Errorf("Failed to create NRI stub: %v", err)
			if maxRetries == -1 || retryCount < maxRetries {
				select {
				case <-time.After(retryDelay):
					retryDelay = calculateBackoff(retryDelay)
					retryCount++
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return err
		}

		klog.Info("Successfully connected to NRI")

		go func(context.Context) {
			<-ctx.Done()
			klog.Info("Shutting down Enforcer......")
			st.Stop()
		}(ctx)

		err = st.Run(ctx)
		if err != nil {
			if err == ctx.Err() {
				return nil
			}
			klog.Errorf("NRI connection lost: %v", err)
			retryCount++
			retryDelay = calculateBackoff(retryDelay)
			if maxRetries == -1 || retryCount < maxRetries {
				klog.Infof("Will retry in %v...", retryDelay)
				select {
				case <-time.After(retryDelay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return err
		}

		return nil
	}

	return nil
}

func calculateBackoff(currentDelay time.Duration) time.Duration {
	newDelay := currentDelay * 2
	if newDelay > maxRetryDelay {
		return maxRetryDelay
	}
	return newDelay
}
