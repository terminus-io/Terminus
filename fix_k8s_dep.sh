#!/bin/bash

# 【关键】修改这里：目标版本设为 v1.32.9
K8S_VERSION="v1.32.9"

# 对应的 library 版本是 v0.32.9
MOD_VERSION=${K8S_VERSION/v1./v0.}

echo "Upgrading go.mod to Kubernetes $K8S_VERSION (Libs: $MOD_VERSION)..."

# 1. 强制要求主库使用 v1.32.9
go mod edit -require=k8s.io/kubernetes@$K8S_VERSION

# 2. 需要替换的 staging 库列表
# 这些库在 k8s 源码里是 v0.0.0，必须 replace 到真实版本 v0.32.9
MODS=(
    api
    apiextensions-apiserver
    apimachinery
    apiserver
    cli-runtime
    client-go
    cloud-provider
    cluster-bootstrap
    code-generator
    component-base
    component-helpers
    controller-manager
    cri-api
    csi-translation-lib
    dynamic-resource-allocation
    endpointslice
    kms
    kube-aggregator
    kube-controller-manager
    kube-proxy
    kube-scheduler
    kubectl
    kubelet
    legacy-cloud-providers
    metrics
    mount-utils
    pod-security-admission
    sample-apiserver
    sample-cli-plugin
    sample-controller
)

# 3. 循环写入 replace
for MOD in "${MODS[@]}"; do
    echo "Replacing k8s.io/${MOD} => k8s.io/${MOD}@${MOD_VERSION}"
    go mod edit -replace="k8s.io/${MOD}=k8s.io/${MOD}@${MOD_VERSION}"
done

# 4. 整理依赖并下载
echo "Running go mod tidy..."
go mod tidy
