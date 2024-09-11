# FAQ

### Q: `mknod: not found` Or `Failed to execute cmd: mknod`
A: You need to make sure `mknod` is available in your image/container.

### Q: How to set CGroup Driver?
A: CGroup Driver can be set in [device-mounter-daemonset.yaml](../../deploy/device-mounter-daemonset.yaml) by environment variable `CGROUP_DRIVER`(default: automatic detection).

### Q: 卸载Ascend NPU时，明明没有使用强制卸载参数`force=true`，还是将正在使用的容器设备卸载掉了
A: 可能是Ascend驱动版本问题，Ascend低版本驱动无法查询到容器设备进程的占用情况导致设备被认为是空闲的。建议升级驱动版本。

经过测试发现：
* 在Mindx v6.0+配套软件版本下：ascend驱动24.1+、cann8.0+，host进程命名空间能检测到容器进程。
版本配套信息：https://www.hiascend.com/document/detail/zh/mindx-dl/60rc2/description/releasenote/mxreleasenote_006.html
* 在Mindx v6.0-配套软件版本下：host进程命名空间无法查询到容器进程，强烈建议升级Mindx配套软件到v6.0+。
版本配套信息：https://www.hiascend.com/document/detail/zh/mindx-dl/501/releasenote/mxreleasenote_002.html

### Q: 为什么调用API成功热卸载了NPU, 其他容器使用这个NPU设备依然报错`dcmi model initialized failed, because the device is used.`
A: 这是因为NPU设备没有开启设备共享模式，默认在设备独占模式下无法真正热卸载掉设备（npu驱动锁定了容器），只有容器销毁才能真正释放设备。

解决办法: 开启npu设备共享模式 

* 查询所有芯片的容器共享模式 命令格式：`npu-smi info -t device-share -i <id>`
* 查询指定芯片的容器共享模式 命令格式：`npu-smi info -t device-share -i <id> -c <chip_id>`
* 设置所有芯片的容器共享模式 命令格式：`npu-smi set -t device-share -i <id> -d 1`
* 设置指定芯片的容器共享模式 命令格式：`npu-smi set -t device-share -i <id> -c <chip_id> -d 1`

如果显示 `This device does not support querying device-share.` 则代表硬件不支持容器设备共享，无法彻底卸载设备

官方文档链接：https://support.huawei.com/enterprise/zh/doc/EDOC1100388866/36b4ef4
