package ext4

import (
	"fmt"
	"os/exec"

	"github.com/Frank-svg-dev/Terminus/pkg/quota"
	"github.com/Frank-svg-dev/Terminus/pkg/utils"
	"k8s.io/klog/v2"
)

type Ext4CLI struct{}

func NewExt4CLI() *Ext4CLI { return &Ext4CLI{} }

func (e *Ext4CLI) SetProjectID(path string, projectID uint32) error {
	args := []string{
		"-R",
		"-p",
		fmt.Sprintf("%d", projectID),
		"+P",
		fmt.Sprintf("%s", path),
	}

	klog.V(4).Infof("执行命令: chattr %v\n", args)

	cmd := exec.Command("chattr", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行失败: %v, 输出: %s", err, string(output))
	}
	return nil
}

func (e *Ext4CLI) SetQuota(projectID uint32, limitBytes uint64) error {
	// 命令格式: setquota -P <ProjectID> <SoftLimit> <HardLimit> <InodesSoft> <InodesHard> <MountPoint>
	block := limitBytes / 1024
	mountPoint, err := utils.GetMountPoint(quota.ContainerdRootPath)
	if err != nil {
		mountPoint = "/"
	}
	args := []string{
		"-P",
		fmt.Sprintf("%d", projectID),
		fmt.Sprintf("%d", block), // 软限制
		fmt.Sprintf("%d", block), // 硬限制
		"0", "0",                 // Inodes 不限制
		mountPoint,
	}

	klog.V(4).Infof("执行命令: setquota %v\n", args)

	cmd := exec.Command("setquota", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行失败: %v, 输出: %s", err, string(output))
	}
	return nil
}

func (e *Ext4CLI) RemoveQuota(dirPath string, projectID uint32) error {
	mountPoint, err := utils.GetMountPoint(dirPath)
	if err != nil {
		mountPoint = "/"
	}
	// 命令: setquota -P <ID> 0 0 0 0 <mountPoint>
	limitArgs := []string{
		"-P",
		fmt.Sprintf("%d", projectID),
		"0", "0", "0", "0",
		mountPoint,
	}

	if out, err := exec.Command("setquota", limitArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("清除配额限制失败: %v, 输出: %s", err, string(out))
	}

	if out, err := exec.Command("chattr", "-p", "0", dirPath).CombinedOutput(); err != nil {
		return fmt.Errorf("重置目录 ProjectID 失败: %v, 输出: %s", err, string(out))
	}

	if out, err := exec.Command("chattr", "-P", dirPath).CombinedOutput(); err != nil {
		return fmt.Errorf("移除目录 Project 属性失败: %v, 输出: %s", err, string(out))
	}

	return nil
}
