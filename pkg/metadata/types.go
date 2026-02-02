package metadata

type ContainerInfo struct {
	ProjectID     uint32 `json:"project_id"`
	Namespace     string `json:"namespace"`
	PodName       string `json:"pod"`
	ContainerName string `json:"container"`
}
