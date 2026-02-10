package nri

import (
	"k8s.io/klog/v2"
)

type Enforcer struct {
	SocketPath string
	PluginName string
	PluginIdx  string
	Hooks      []Hook
}

type Option func(*Enforcer)

func WithSocketPath(p string) Option { return func(e *Enforcer) { e.SocketPath = p } }
func WithPluginName(n string) Option { return func(e *Enforcer) { e.PluginName = n } }
func WithPluginIdx(i string) Option  { return func(e *Enforcer) { e.PluginIdx = i } }

func WithHook(h Hook) Option {
	return func(e *Enforcer) {
		klog.InfoS("Plugin register", "name", h.Name())
		e.Hooks = append(e.Hooks, h)
	}
}
