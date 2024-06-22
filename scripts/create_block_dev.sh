#!/bin/bash

usage() {
    echo "Usage: $0 block_filepath block_size(MB) mount_directory"
    echo "Example: create a 10mb block device /tmp/myblock and mount it to the directory /tmp/test"
    echo "bash $0 /tmp/myblock 10 /tmp/test"
    exit 1
}

if [ $# -ne 3 ]; then
    usage
fi

BLOCK_FILEPATH=$1
BLOCK_SIZE=$2
MOUNT_DIR=$3

# 验证BLOCK_SIZE是否为正整数
if ! [[ "$BLOCK_SIZE" =~ ^[0-9]+$ ]]; then
    echo "Error: Block size must be a positive integer."
    exit 1
fi

# 检查路径是否存在
if [ -e "$BLOCK_FILEPATH" ]; then
    echo "Error: Block file path $BLOCK_FILEPATH already exists!"
    exit 1
fi


if [ -e "$MOUNT_DIR" ]; then
    echo "Error: Mount Directory $MOUNT_DIR already exists!"
    exit 1
fi

# 创建块设备文件
dd if=/dev/zero of="$BLOCK_FILEPATH" bs=1M count="$BLOCK_SIZE" || {
    echo "Failed to create block file $BLOCK_FILEPATH."
    exit 1
}

# 格式化为ext4
mkfs.ext4 "$BLOCK_FILEPATH" || {
    echo "Failed to format $BLOCK_FILEPATH as ext4."
    exit 1
}

# 创建挂载点
mkdir -p "$MOUNT_DIR" || {
    echo "Failed to create mount dir $MOUNT_DIR."
    exit 1
}

# 挂载文件系统
mount -o loop "$BLOCK_FILEPATH" "$MOUNT_DIR" || {
    echo "Failed to mount $BLOCK_FILEPATH to $MOUNT_DIR."
    exit 1
}

echo "Successfully mounted $BLOCK_FILEPATH to $MOUNT_DIR."