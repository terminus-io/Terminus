package quota

import "github.com/Frank-svg-dev/Terminus/pkg/quota/xfs"

type QuotaManager interface {
	SetProjectID(path string, projectID uint32) error
	SetQuota(projectID uint32, limitBytes uint64) error
	FetchAllReports(mountPoint string, typeFlag string) (map[uint32]xfs.QuotaReport, error)
	RemoveQuota(dirPath string, projectID uint32) error
}
