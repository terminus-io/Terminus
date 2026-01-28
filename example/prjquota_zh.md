
# Linux 开启 Project Quota (Prjquota) 完整指南 🐧

**Project Quota** 允许你基于“目录树”（Project ID）而不是用户或组来限制磁盘使用量。这对容器化环境（如 Docker/Kubernetes）限制容器层存储至关重要。

---

## 1. Ubuntu / Debian (针对 Ext4 文件系统)

Ubuntu 默认使用 **Ext4** 文件系统。在 Ext4 上开启 Project Quota 需要两个步骤：启用文件系统特性 + 添加挂载选项。

### 步骤 1：安装配额工具

```bash
sudo apt-get update
sudo apt-get install quota
```

### 步骤 2：检查并启用 Ext4 的 project 特性

Ext4 默认可能未开启 project quota 支持。你需要对**卸载状态**的设备运行 `tune2fs`。请根据你的目标分区类型选择对应的方法。

#### 场景 A：目标是非根目录（普通数据盘）

1. **卸载设备** (如果无法卸载，需停止占用该磁盘的进程)：
```bash
# 假设目标设备是 /dev/sdb1
sudo umount /dev/sdb1
```


2. **启用 project 特性**：
```bash
sudo tune2fs -O project,quota /dev/sdb1
```


3. **验证是否开启**：
```bash
sudo tune2fs -l /dev/sdb1 | grep "Filesystem features"
# 输出中应包含 "project" 和 "quota"
```



#### 场景 B：目标是根目录 (Root Partition) 🛑

由于根目录无法在系统运行时卸载，需要借助 **InitRamfs** 在启动阶段进行操作。

1. **创建 Initramfs Hook 脚本**：
将 `tune2fs` 和 `e2fsck` 命令拷贝进 Initramfs，否则启动环境默认无此命令。
*(注：使用 cat 写入文件)*
```bash
cat <<'EOF' | sudo tee /etc/initramfs-tools/hooks/add-tune2fs
#!/bin/sh
PREREQ=""
prereqs() { echo "$PREREQ"; }
case $1 in prereqs) prereqs; exit 0 ;; esac
. /usr/share/initramfs-tools/hook-functions
copy_exec /sbin/tune2fs /sbin
copy_exec /sbin/e2fsck /sbin
EOF

# 给予执行权限
sudo chmod +x /etc/initramfs-tools/hooks/add-tune2fs
```


2. **更新 Initramfs 镜像**：
```bash
sudo update-initramfs -u
```


3. **触发强制磁盘检查**：
设置开机自动执行一次 fsck（因为修改磁盘元数据建议进行检查）。
```bash
sudo touch /forcefsck
```


4. **重启进入 Initramfs Shell**：
重启机器。在 Grub 界面选择 Advanced options，或编辑启动项添加 `break=premount` 进入命令行模式。如果之前的配置正确，fsck 可能会暂停启动，允许你操作。
5. **在 Initramfs 环境中执行特性开启**：
进入 Shell 后执行以下命令（注意：此时路径可能是 LVM 路径）：
```bash
# 示例路径，请根据实际情况修改
tune2fs -O project,quota /dev/mapper/ubuntu--vg-ubuntu--lv
```


6. **再次重启机器**：
```bash
reboot -f
```



### 步骤 3：修改 `/etc/fstab`

编辑 `/etc/fstab` 文件，在挂载选项中添加 `prjquota`。

```bash
# 修改前
UUID=xxxx-xxxx  /data  ext4  defaults  0  2

# 修改后 (添加 prjquota)
UUID=xxxx-xxxx  /data  ext4  defaults,prjquota  0  2
```

### 步骤 4：重新挂载并生效

```bash
# 重新挂载
sudo mount -o remount /data
# 或者如果之前卸载了
sudo mount -a

# 激活配额
sudo quotaon -Pvp /data
```

---

## 2. Red Hat / CentOS / AlmaLinux (针对 XFS 文件系统)

RHEL 系列默认使用 **XFS** 文件系统。XFS 原生支持 Project Quota，不需要转换特性，但根据是否是“根目录”有不同的配置方式。

### 步骤 1：安装配额工具

```bash
sudo dnf install quota
```

### 场景 A：开启 **非根目录** (如 /var, /home, /data)

直接修改 `/etc/fstab` 即可。

1. **编辑 `/etc/fstab**`，在 defaults 后面加上 `prjquota` (或 `pquota`)：
```bash
# 修改前
/dev/mapper/data-lv  /var/lib/containerd  xfs  defaults  0  0

# 修改后
/dev/mapper/data-lv  /var/lib/containerd  xfs  defaults,prjquota  0  0
```


2. **重新挂载**：
```bash
sudo mount -o remount /var/lib/containerd
```



### 场景 B：开启 **根目录 (/)**

> ⚠️ **注意：** 开启根目录配额仅修改 fstab 无效，因为根文件系统在读取 fstab 前已挂载。必须修改 GRUB。

1. **编辑 GRUB 配置** (`/etc/default/grub`)：
找到 `GRUB_CMDLINE_LINUX` 这一行，在末尾添加 `rootflags=prjquota`。
```bash
GRUB_CMDLINE_LINUX="... crashkernel=auto ... rootflags=prjquota"
```


2. **重新生成 GRUB 配置**：
* **Legacy BIOS 启动**：
```bash
sudo grub2-mkconfig -o /boot/grub2/grub.cfg
```


* **UEFI 启动**：
```bash
sudo grub2-mkconfig -o /boot/efi/EFI/centos/grub.cfg
```




3. **重启系统**：
```bash
sudo reboot
```



---

## 3. 验证是否开启成功 ✅

无论使用哪种系统，操作完成后请执行以下检查。

### 方法 1：查看挂载选项

```bash
mount | grep prjquota
```

*预期输出中应包含 `prjquota` (或者 `project`)。*

### 方法 2：实际测试 (通用)

```bash
# 1. 在目标目录下创建一个测试目录
mkdir -p /path/to/mount/test-quota

# 2. 为该目录分配一个 Project ID (例如 100)
sudo chattr +P -p 100 /path/to/mount/test-quota

# 3. 检查属性是否设置成功 (输出应带 'P')
lsattr -p /path/to/mount/

# 4. 尝试查看配额报告 (确保无报错)
sudo repquota -P /path/to/mount
```
---

## 4. 常见问题排查 (Troubleshooting) 🔧

* **报错 `mount: /point not mounted or bad option**`
* 检查 `/etc/fstab` 拼写是否为 `prjquota`。
* 如果是 Ext4，检查是否忘记执行 `tune2fs -O project`。


* **报错 `setquota: Mountpoint (or device) not found**`
* 这通常发生在容器内。在容器或脚本中操作时，**请直接指定物理设备路径** (如 `/dev/sdX` 或 `/dev/mapper/xxx`) 而不是挂载点目录。


* **XFS 无法开启**
* XFS 的配额一旦开启，只能通过卸载并去掉挂载参数来关闭。
* 如果修改 fstab 后没生效，且无法卸载（如根目录），必须重启系统。
