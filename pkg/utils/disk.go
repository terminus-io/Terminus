package utils

import (
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
