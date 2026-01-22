package nri

import (
	"context"

	"github.com/containerd/nri/pkg/stub"
	"k8s.io/klog/v2"
)

func NewEnforcer(opts ...Option) (*Enforcer, error) {

	e := &Enforcer{
		// SocketPath: "/var/run/nri/nri.sock",
		// PluginName: "terminus",
		// PluginIdx:  "00",
	}
	for _, opt := range opts {
		opt(e)
	}
	return e, nil
}

func (e *Enforcer) Run(ctx context.Context) error {
	opts := []stub.Option{
		stub.WithPluginName(e.PluginName),
		stub.WithPluginIdx(e.PluginIdx),
		stub.WithSocketPath(e.SocketPath),
	}
	st, err := stub.New(e, opts...)
	if err != nil {
		return err
	}

	go func(context.Context) {
		<-ctx.Done()
		klog.Info("Shutting down Enforcer......")
		st.Stop()
	}(ctx)

	if err := st.Run(ctx); err != nil {
		if err == ctx.Err() {
			return nil
		}
	}

	return err
}
