# FAQ

### Q: `mknod: not found` Or `Failed to execute cmd: mknod`
A: You need to make sure `mknod` is available in your image/container.

### Q: How to set CGroup Driver?
A: CGroup Driver can be set in [device-mounter-daemonset.yaml](../../deploy/device-mounter-daemonset.yaml) by environment variable `CGROUP_DRIVER`(default: automatic detection).

### Q: 卸载Ascend NPU时，明明没有使用强制卸载参数`force=true`，还是将正在使用的容器设备卸载掉了
A: 可能是Ascend驱动版本问题，Ascend低版本驱动无法查询到容器设备进程的占用情况导致设备被认为是空闲的。建议升级驱动版本。

经过测试发现：
* 在Mindx v6.0+配套软件版本下：也就是ascend驱动24.1+、cann8.0+， host进程命名空间能检测到容器进程。
版本配套信息： https://www.hiascend.com/document/detail/zh/mindx-dl/60rc2/description/releasenote/mxreleasenote_006.html
* Mindx v6.0-配套软件版本下：host进程命名空间无法查询到容器进程，强烈建议升级Mindx配套软件到v6.0+。
版本配套信息： https://www.hiascend.com/document/detail/zh/mindx-dl/501/releasenote/mxreleasenote_002.html