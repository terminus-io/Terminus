package metadata

type ContainerInfo struct {
	ProjectID     uint32       `json:"project_id"`
	Namespace     string       `json:"namespace"`
	PodName       string       `json:"pod"`
	ContainerName string       `json:"container"`
	VolumeName    string       `json:"volume_name"`
	StorageType   STORAGE_TYPE `json:"storage_type"`
}

type STORAGE_TYPE string

const (
	ROOTFS_TYPE   STORAGE_TYPE = "rootfs"
	EMPTYDIR_TYPE STORAGE_TYPE = "emptyDir"
)
