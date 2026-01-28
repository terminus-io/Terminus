package xfs

import (
	"fmt"
	"os/exec"

	"k8s.io/klog/v2"
)

type XFSCLI struct{}

func NewXFSCLI() *XFSCLI { return &XFSCLI{} }

func (m *XFSCLI) SetProjectID(path string, projectID uint32) error {
	klog.V(4).InfoS("Exec: SetProjectID", "path", path, "id", projectID)
	// MVP: 调用 xfs_quota 命令
	cmd := exec.Command("xfs_quota", "-x", "-c", fmt.Sprintf("project -s -p %s %d", path, projectID), "/var/lib/containerd")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set project ID: %v, out: %s", err, string(out))
	}
	return nil
}

func (m *XFSCLI) SetQuota(projectID uint32, limitBytes uint64) error {
	klog.V(4).InfoS("Exec: SetQuota", "id", projectID, "limit", limitBytes, "MB")
	// MVP: 调用 xfs_quota 命令
	cmd := exec.Command("xfs_quota", "-x", "-c", fmt.Sprintf("limit -p bhard=%d %d", limitBytes, projectID), "/var/lib/containerd")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set quota: %v, out: %s", err, string(out))
	}
	return nil
}

func (m *XFSCLI) RemoveQuota(dirPath string, projectID uint32) error {

	cmdStr := fmt.Sprintf("limit -p bsoft=0 bhard=0 isoft=0 ihard=0 %d", projectID)

	cmd := exec.Command("xfs_quota", "-x", "-c", cmdStr, "/var/lib/containerd")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove quota for id %d: %s, %w", projectID, string(output), err)
	}

	return nil
}
