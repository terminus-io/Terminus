package xfs

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/terminus-io/Terminus/pkg/quota"
)

func (e *XFSCLI) FetchAllReports(mountPoint string, typeFlag string) (map[uint32]quota.QuotaReport, error) {
	cmdStr := fmt.Sprintf("report -p -n -N -%s", typeFlag)
	cmd := exec.Command("xfs_quota", "-x", "-c", cmdStr, mountPoint)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("xfs_quota report failed: %v", err)
	}

	reports := make(map[uint32]quota.QuotaReport)
	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		idStr := strings.TrimPrefix(fields[0], "#")
		idUint, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			continue
		}

		// 解析 Used 和 Limit
		// 注意：xfs_quota 输出列数可能因版本而异，需做容错处理
		// 通常: ID Used Soft Hard ...
		used, _ := strconv.ParseUint(fields[1], 10, 64)

		var limit uint64
		if len(fields) >= 4 {
			limit, _ = strconv.ParseUint(fields[3], 10, 64)
		} else if len(fields) == 3 {
			limit, _ = strconv.ParseUint(fields[2], 10, 64)
		}

		reports[uint32(idUint)] = quota.QuotaReport{
			ID:    uint32(idUint),
			Used:  used,
			Limit: limit,
		}
	}
	return reports, nil
}
