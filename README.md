
# 🏛️ Terminus

<img width="2816" height="1536" alt="Gemini_Generated_Image_3r4t1l3r4t1l3r4t" src="https://github.com/user-attachments/assets/83c7772a-bac4-46eb-8ced-bd39d6d26901" />


<p align="center">
  <b>The Boundary God for Kubernetes Ephemeral Storage.</b>
</p>

<p align="center">
  <a href="https://kubernetes.io"><img src="https://img.shields.io/badge/kubernetes-%3E%3D1.24-326ce5?style=flat-square&logo=kubernetes" alt="Kubernetes"></a>
  <a href="https://github.com/containerd/nri"><img src="https://img.shields.io/badge/component-NRI-orange?style=flat-square" alt="NRI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square" alt="License"></a>
  <a href="#"><img src="https://img.shields.io/badge/build-passing-brightgreen?style=flat-square" alt="Build Status"></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/language-Go-00ADD8?style=flat-square&logo=go" alt="Go"></a>
</p>

---

## 📖 Introduction

**Terminus** is a cloud-native storage governance system designed to secure Kubernetes nodes from **Rootfs/Overlayfs exhaustion**.

In standard Kubernetes, ephemeral storage limits (`requests.ephemeral-storage`) are soft limits enforced by periodic Kubelet scanning (`du`). This mechanism is IO-intensive, slow to react, and ineffective against rapid disk consumption, often leading to node instability ("Noisy Neighbor" problems).

**Terminus** solves this by enforcing **Hard Limits** at the Linux kernel level using **Project Quota**. It also introduces a **Disk-Aware Scheduler** to balance I/O pressure based on real disk usage, ensuring node stability under high load.

> *"Terminus, the Roman god of boundaries, yields to no one."*

## 🚀 Key Features

* **🛡️ Kernel-Level Isolation (Terminus-Enforcer)**
  Enforce strict disk usage limits on container Rootfs using XFS/Ext4 Project Quota via **NRI (Node Resource Interface)**. Zero overhead, immediate enforcement.

* **🧠 Disk-Aware Scheduling (Terminus-Scheduler)**
  A scheduler plugin that filters and scores nodes based on **Real Physical Usage** and configurable **Over-provisioning Rates**. It prevents scheduling pods to nodes that are physically dangerously full, regardless of their allocation status.

* **⚡ Active Protection (Terminus-Exporter)**
  An efficient node agent that monitors Project ID usage and triggers graceful
## 🏗️ Architecture

Terminus consists of three micro-components working in harmony:

```mermaid
graph TD
    User((User)) -->|Annotation: storage.terminus.io/size: '10Gi'| API[K8s API Server]
    
    subgraph Control Plane
        API -->|Watch| Scheduler[Kube-Scheduler]
        Scheduler -- Filter/Score --> Plugin[<b>Terminus-Scheduler</b>]
    end

    subgraph Worker Node
        API -->|Schedule| Kubelet
        Kubelet --> CRI[Containerd]
        CRI -- Hook --> NRI[<b>Terminus-Enforcer</b>]
        NRI -- Set Quota --> Kernel[Linux Kernel<br/>Project Quota]
        
        Kernel -.-> Pod[Container Rootfs]
        
        Agent[<b>Terminus-Exporter</b>] -- Watch Usage --> Kernel
    end
    
    classDef component fill:#f9f,stroke:#333,stroke-width:2px;
    class NRI,Plugin,Agent component;

```

## 🛠️ Prerequisites

Before installing Terminus, ensure your environment meets the following requirements:

* **Kubernetes**: v1.24+ (Requires Containerd with NRI support enabled).
* **Container Runtime**: Containerd v1.7+.
* **Filesystem**: The backend filesystem for `/var/lib/containerd` must be **XFS** or **Ext4** with Project Quota enabled (`prjquota` mount option).

## 📦 Installation

### 1. Enable NRI in Containerd

Edit your `/etc/containerd/config.toml` to enable the NRI plugin:

```toml
[plugins."io.containerd.nri.v1.nri"]
  disable = false
  disable_connections = false
  plugin_config_path = "/etc/nri/conf.d"
  plugin_path = "/opt/nri/plugins"
  socket_path = "/var/run/nri/nri.sock"

```

*Restart containerd after editing.*

### 2. Install Terminus via Helm

```bash
helm install terminus ./chart -n terminus --create-namespace
```

### 3. Manual Installation

```bash
# Install the RBAC
kubectl apply -f deploy/manifests/rbac.yaml

# Install the Scheduler Configmap
kubectl create cm -n terminus --from-file=deploy/manifests/terminus-scheduler-config.yaml

# Install the Node Agent (Enforcer & Exporter)
kubectl apply -f deploy/manifests/terminus-enforcer.yaml

# Install the Scheduler Plugin
kubectl apply -f deploy/manifests/terminus-scheduler.yaml

```

## 💻 Usage

### 1. Enforcing Limits via Annotation

Simply add the `storage.terminus.io/size` or `storage.terminus.io/size.${containerName}` annotation to your Pod Or A specific container within a Pod. Terminus will automatically inject the Project Quota limit.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
  annotations:
    # Limit the Rootfs (Overlayfs) to 10Gi (Hard Limit)
    storage.terminus.io/size: "10Gi"
    storage.terminus.io/size.nginx: "5Gi"
spec:
  containers:
  - name: nginx
    image: nginx

```

### 2. Configuring Scheduling Policy

You can configure the `Terminus-Scheduler` via ConfigMap to set the over-provisioning strategy.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: terminus-scheduler-config
  namespace: terminus
data:
  terminus-scheduler-config.yaml: |
    apiVersion: kubescheduler.config.k8s.io/v1
    kind: KubeSchedulerConfiguration
    leaderElection:
      leaderElect: true
      resourceName: terminus-scheduler
      resourceNamespace: terminus
    profiles:
      - schedulerName: terminus-scheduler
        plugins:
          filter:
            enabled:
              - name: terminus-scheduler
          score:
            enabled:
              - name: terminus-scheduler
                weight: 1
    pluginConfig:
      - name: terminus-scheduler
        args:
          oversubscriptionRatio: 1.5

```

## Grafana Dashboard
![alt text](./image/grafana_dashboard.png)

## 🗺️ Roadmap

* [x] **v0.1 (MVP)**: NRI plugin implementation for XFS Project Quota.
* [x] **v0.2**: Prometheus Exporter & Grafana Dashboard integration.
* [x] **v1.0**: Scheduler Plugin with "Real Usage" awareness.

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](https://www.google.com/search?q=CONTRIBUTING.md) for details on how to submit a PR.

1. Fork the repo.
2. Create your feature branch (`git checkout -b feature/amazing-feature`).
3. Commit your changes.
4. Push to the branch.
5. Open a Pull Request.

## 📄 License

Distributed under the Apache 2.0 License. See `LICENSE` for more information.
