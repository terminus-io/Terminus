package cmd

import (
	"context"
	"errors"
	"flag"
	"os/signal"
	"syscall"
	"time"

	"github.com/Frank-svg-dev/Terminus/pkg/exporter"
	"github.com/Frank-svg-dev/Terminus/pkg/hooks"
	"github.com/Frank-svg-dev/Terminus/pkg/k8s"
	"github.com/Frank-svg-dev/Terminus/pkg/metadata"
	"github.com/Frank-svg-dev/Terminus/pkg/nri"
	"github.com/Frank-svg-dev/Terminus/pkg/quota/xfs"
	"github.com/Frank-svg-dev/Terminus/pkg/reporter"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
)

const (
	socketPath = "/var/run/nri/nri.sock"
	pluginName = "Terminus-Enforcer"
	pluginIdx  = "06"
)

// rootCmd 定义根命令
var rootCmd = &cobra.Command{
	Use:   "terminus-enforcer",
	Short: "Terminus NRI Plugin",
	Long:  `Terminus Enforcer listens to NRI events and applies Project Quota limits.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// 确保 klog 能够解析 flags
		flag.Parse()
	},
	// 真正的执行入口
	RunE: func(cmd *cobra.Command, args []string) error {

		// 1. 基础资源初始化 (线性执行)
		kClient, err := k8s.GenrateK8sClient()
		if err != nil {
			return err
		}

		// 2. 组件初始化
		store := metadata.NewAsyncStore(1000)
		qm := xfs.NewXFSCLI()
		storageHook := hooks.NewStorageHook(qm, store)

		enforcer, err := nri.NewEnforcer(
			nri.WithSocketPath(socketPath),
			nri.WithPluginName(pluginName),
			nri.WithPluginIdx(pluginIdx),
			nri.WithK8sClient(kClient),
			nri.WithHook(storageHook),
		)
		if err != nil {
			return err
		}

		rpt := reporter.NewReporter(store, kClient, 30*time.Second)

		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		g, ctx := errgroup.WithContext(ctx)

		// A. 启动 Metadata Store
		g.Go(func() error {
			klog.Info("Starting Metadata Store...")
			store.Run(ctx) // 假设 store.Run 是阻塞的，或者你需要简单封装一下
			return nil
		})

		// B. 启动 Reporter
		g.Go(func() error {
			klog.Info("Starting Reporter...")
			rpt.Run(ctx) // 假设 reporter.Run 会监听 ctx.Done() 并退出
			return nil
		})

		g.Go(func() error {
			return exporter.StartMetricsServer(ctx, store, ":9201")
		})

		// D. 启动 Enforcer (核心进程)
		g.Go(func() error {
			klog.Info("Starting NRI Enforcer...")
			return enforcer.Run(ctx)
		})

		// 4. 等待所有组件退出
		// 只要上述任意一个 g.Go 返回 error，或者收到 SIGTERM，Wait 就会返回
		if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			klog.ErrorS(err, "Terminus Enforcer exited with error")
			return err
		}

		klog.Info("Terminus Enforcer stopped gracefully")

		return nil
	},
}

// Execute 是 main.go 调用的函数
func Execute() error {
	return rootCmd.Execute()
}

// init 初始化 Flags
func init() {
	klog.InitFlags(nil)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	_ = flag.Set("logtostderr", "true")
}
