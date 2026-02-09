package cmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"github.com/terminus-io/Terminus/pkg/exporter"
	ext4_exporter "github.com/terminus-io/Terminus/pkg/exporter/ext4"
	"github.com/terminus-io/Terminus/pkg/hooks"
	"github.com/terminus-io/Terminus/pkg/k8s"
	"github.com/terminus-io/Terminus/pkg/metadata"
	"github.com/terminus-io/Terminus/pkg/nri"
	"github.com/terminus-io/Terminus/pkg/quota"
	"github.com/terminus-io/Terminus/pkg/quota/ext4"
	"github.com/terminus-io/Terminus/pkg/quota/xfs"
	"github.com/terminus-io/Terminus/pkg/reporter"
	"github.com/terminus-io/Terminus/pkg/utils"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
)

const (
	socketPath       = "/var/run/nri/nri.sock"
	pluginName       = "Terminus-Enforcer"
	pluginIdx        = "06"
	EXT4_SUPER_MAGIC = 0xEF53
	XFS_SUPER_MAGIC  = 0x58465342
	containerdPath   = "/var/lib/containerd"
)

// rootCmd 定义根命令
var rootCmd = &cobra.Command{
	Use:   "terminus-enforcer",
	Short: "Terminus NRI Plugin",
	Long:  `Terminus Enforcer listens to NRI events and applies Project Quota limits.`,
	RunE: func(cmd *cobra.Command, args []string) error {

		for !checkContainerdRootPathQuotaEnabled() {
			klog.Warning("Waiting for /var/lib/containerd to have prjquota enabled...")
			time.Sleep(5 * time.Second)
		}

		if os.Getenv("NODE_NAME") == "" {
			return errors.New("NODE_NAME var is empty, please export NODE_NAME before")
		}

		kClient, err := k8s.GenrateK8sClient()
		if err != nil {
			return err
		}

		store := metadata.NewAsyncStore(1000, kClient)
		qm, collector, err := genrateQuotaManager(containerdPath, store)
		if err != nil {
			return err
		}

		go func() {
			store.TriggerRestore()
		}()

		storageHook := hooks.NewStorageHook(qm, store, kClient)

		enforcer, err := nri.NewEnforcer(
			nri.WithSocketPath(socketPath),
			nri.WithPluginName(pluginName),
			nri.WithPluginIdx(pluginIdx),
			nri.WithHook(storageHook),
		)
		if err != nil {
			return err
		}

		rpt := reporter.NewReporter(store, kClient, 30*time.Second)

		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		g, ctx := errgroup.WithContext(ctx)

		g.Go(func() error {
			klog.Info("Starting Metadata Store...")
			store.Run(ctx)
			return nil
		})

		g.Go(func() error {
			klog.Info("Starting Reporter...")
			rpt.Run(ctx)
			return nil
		})

		g.Go(func() error {
			return exporter.StartMetricsServer(ctx, collector, store, ":9201")
		})

		g.Go(func() error {
			klog.Info("Starting NRI Enforcer...")
			return enforcer.Run(ctx)
		})

		if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			klog.ErrorS(err, "Terminus Enforcer exited with error")
			return err
		}

		klog.Info("Terminus Enforcer stopped gracefully")

		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	klog.InitFlags(nil)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	_ = flag.Set("logtostderr", "true")
}

func genrateQuotaManager(path string, store *metadata.AsyncStore) (quota.QuotaManager, prometheus.Collector, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil, nil, err
	}
	fsType := int64(stat.Type)
	switch fsType {
	case EXT4_SUPER_MAGIC:
		klog.Info("this node filesystem is ext4")
		mountPoint, _ := utils.GetMountPoint(containerdPath)
		return ext4.NewExt4CLI(), ext4_exporter.NewExt4Collector(mountPoint, store), nil
	case XFS_SUPER_MAGIC:
		klog.Info("this node filesystem is xfs")
		return xfs.NewXFSCLI(), exporter.NewXFSCollector(containerdPath, store), nil
	default:
		return nil, nil, fmt.Errorf("Unknown or other FS. Magic Number: %x\nSupport fileSystem: xfs / ext4", fsType)
	}
}

func checkContainerdRootPathQuotaEnabled() bool {
	data, _ := os.ReadFile("/proc/mounts")
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[1] == containerdPath {
			opts := "," + fields[3] + ","
			return strings.Contains(opts, ",prjquota,")
		}
	}
	return false
}
