# FAQ

### Q: `mknod: not found` Or `Failed to execute cmd: mknod`
A: You need to make sure `mknod` is available in your image/container.

### Q: How to set CGroup Driver?
A: CGroup Driver can be set in [device-mounter-daemonset.yaml](../../deploy/device-mounter-daemonset.yaml) by environment variable `CGROUP_DRIVER`(default: automatic detection).

