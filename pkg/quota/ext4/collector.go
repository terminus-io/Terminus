package ext4

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Frank-svg-dev/Terminus/pkg/quota/xfs"
)

func (e *Ext4CLI) FetchAllReports(mountPoint string, typeFlag string) (map[uint32]xfs.QuotaReport, error) {
	args := []string{"-P", "-n", mountPoint}

	cmd := exec.Command("repquota", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 某些系统如果没有 active quota，repquota 可能会返回非 0
		// 这里视情况处理，通常返回空 map 或错误
		return nil, fmt.Errorf("执行 repquota 失败: %v, 输出: %s", err, string(output))
	}

	reports := make(map[uint32]xfs.QuotaReport)
	scanner := bufio.NewScanner(bytes.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过干扰行：空行、表头(Project/Block)、分隔符(---)
		if line == "" || strings.HasPrefix(line, "Project") || strings.HasPrefix(line, "Block") || strings.HasPrefix(line, "-") {
			continue
		}

		// repquota -P -n 输出典型格式 (空格分隔):
		// #9999   --   102400   204800   204800   10   20   20
		// 下标:   0(ID)   1(Stat)  2(Used)  3(Soft)  4(Hard) ...

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		// 1. 解析 ID (去掉可能存在的 # 前缀)
		idStr := strings.TrimPrefix(fields[0], "#")
		idVal, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			continue // 解析 ID 失败则跳过该行
		}

		// 2. 解析使用量和限制
		// repquota 输出的单位通常是 1KB Blocks
		usedBlocks, _ := strconv.ParseUint(fields[2], 10, 64)
		// field[3] 是软限制，field[4] 是硬限制
		// 根据你的结构体定义，Limit 通常指 Hard Limit
		hardLimitBlocks, _ := strconv.ParseUint(fields[4], 10, 64)

		reports[uint32(idVal)] = xfs.QuotaReport{
			ID:    uint32(idVal),
			Used:  usedBlocks * 1024,      // 转换为 Bytes
			Limit: hardLimitBlocks * 1024, // 转换为 Bytes
		}
	}

	return reports, nil
}
