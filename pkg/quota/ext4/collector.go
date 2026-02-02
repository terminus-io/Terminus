package ext4

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Frank-svg-dev/Terminus/pkg/quota"
)

func (e *Ext4CLI) FetchAllReports(mountPoint string, typeFlag string) (map[uint32]quota.QuotaReport, error) {
	args := []string{"-P", "-n", mountPoint}

	cmd := exec.Command("repquota", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("执行 repquota 失败: %v, 输出: %s", err, string(output))
	}

	reports := make(map[uint32]quota.QuotaReport)
	scanner := bufio.NewScanner(bytes.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
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

		idStr := strings.TrimPrefix(fields[0], "#")
		idVal, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			continue
		}

		usedBlocks, _ := strconv.ParseUint(fields[2], 10, 64)
		hardLimitBlocks, _ := strconv.ParseUint(fields[4], 10, 64)

		reports[uint32(idVal)] = quota.QuotaReport{
			ID:    uint32(idVal),
			Used:  usedBlocks * 1024,
			Limit: hardLimitBlocks * 1024,
		}
	}

	return reports, nil
}
