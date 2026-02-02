package utils

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// DiskStatus 用于存储磁盘空间信息 (单位: 字节)
type DiskStatus struct {
	Total     uint64 // 总容量
	Used      uint64 // 已使用
	Free      uint64 // 剩余可用 (对非 root 用户)
	Avail     uint64 // 剩余可用 (对 root 用户，通常和 Free 一样，但有些系统会保留部分给 root)
	BlockSize uint64 // 块大小
}

// GetDiskUsage 获取指定目录所在磁盘/分区的空间使用情况
func GetDiskUsage(path string) (DiskStatus, error) {
	fs := syscall.Statfs_t{}

	// 执行系统调用
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return DiskStatus{}, err
	}

	ds := DiskStatus{}
	blockSize := uint64(fs.Bsize)

	ds.Total = fs.Blocks * blockSize
	ds.Free = fs.Bfree * blockSize
	ds.Used = ds.Total - ds.Free
	ds.BlockSize = blockSize

	return ds, nil
}

func GetMountPoint(path string) (string, error) {
	cmd := exec.Command("df", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	//解析输出
	// /dev/mapper/xxx      79738176  11561608  68160184  15% /dev/termination-log
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("unexpected df output format")
	}

	// 获取最后一行
	lastLine := lines[len(lines)-2]

	//获取第一列（即设备路径）
	fields := strings.Fields(lastLine)
	if len(fields) > 0 {
		return fields[0], nil
	}

	return "", fmt.Errorf("could not parse device path")
}
