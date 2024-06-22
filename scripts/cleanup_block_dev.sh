#!/bin/bash

# 使用方法说明
usage() {
    echo "Usage: $0 mount_directory [cleanup]"
    echo "Example: To unmount only: bash $0 /tmp/test"
    echo "Example: To unmount and cleanup: bash $0 /tmp/test cleanup"
    exit 1
}

# 参数检查
if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    usage
fi

MOUNT_DIR=$1
CLEANUP=$2

# 检查挂载点是否存在且已被挂载
if ! mountpoint -q "$MOUNT_DIR"; then
    echo "Error: $MOUNT_DIR is not a mount point or not currently mounted."
    exit 1
fi

# 卸载挂载点
umount "$MOUNT_DIR" || {
    echo "Failed to unmount $MOUNT_DIR."
    exit 1
}
echo "Successfully unmounted $MOUNT_DIR."

# 清理块设备文件和挂载目录（如果请求）
# TODO 无法检测清理回环设备
if [ "$CLEANUP" = "cleanup" ]; then
    # 假设块设备文件路径是通过检查挂载信息获取的，这里简化处理，实际应用中可能需要更复杂的逻辑来确定块设备文件
    BLOCK_DEVICE=$(grep "$MOUNT_DIR" /proc/mounts | awk '{print $1}')
    if [ -n "$BLOCK_DEVICE" ]; then
        echo "Cleaning up block device $BLOCK_DEVICE and mount directory $MOUNT_DIR..."
        rm -f "$BLOCK_DEVICE"
        rmdir "$MOUNT_DIR"
        echo "Cleanup completed."
    else
        echo "Unable to determine block device associated with $MOUNT_DIR for cleanup."
        exit 1
    fi
fi

exit 0