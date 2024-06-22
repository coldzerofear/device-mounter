#!/bin/sh
# This script dynamically mounts host volumes to a container by specifying its PID

usage() {
    echo "Usage: $0 container_pid host_path container_path"
    echo "Example: To mount /tmp/test to a container's /src with PID 12345:"
    echo "bash $0 12345 /tmp/test /src"
    exit 1
}

if [ $# -ne 3 ]; then
    usage
fi

which nsenter &>/dev/null || { echo "Please install nsenter: yum install util-linux"; exit 1; }

set -e

CONTAINER_PID=$1
HOSTPATH=$(realpath $2)
CONTPATH=$3

if ! kill -0 "$CONTAINER_PID" 2>/dev/null; then
    echo "Container PID $CONTAINER_PID not found."
    exit 1
fi

if [ ! -d "$HOSTPATH" ]; then
    echo "Directory $HOSTPATH does not exist!"
    exit 1
fi

# Simplify finding the filesystem of the host path
FILESYS=$(df --output=source "$HOSTPATH" | tail -n 1 | tr -d '[:space:]')

if [ -z "$FILESYS" ]; then
    echo "Failed to determine the filesystem for $HOSTPATH"
    exit 1
fi

run_command() {
    sudo nsenter --target $CONTAINER_PID --mount --uts --ipc --net --pid -- sh -c "$1"
}

# Ensure the filesystem is mounted within the container's namespace
if [ $(run_command "grep -c $FILESYS /proc/mounts") -eq 0 ]; then
    echo "The filesystem $FILESYS is not mounted in the container's namespace."
    exit 1
fi

# Check if the container path is already in use
if [ $(run_command "grep -c $CONTPATH /proc/self/mounts") -ne 0 ]; then
    echo "Container with PID $CONTAINER_PID has $CONTPATH already in use!"
    exit 1
fi

# Create a temporary mount point and perform bind mount
tmpPath="/tmpmnt_$(date +%s)"
if ! $(run_command "[ -d $tmpPath ] || mkdir -p $tmpPath"); then
    echo "Failed to create temp dir in container."
    exit 1
fi

if ! $(run_command "[ -d $CONTPATH ] || mkdir -p $CONTPATH"); then
    echo "Failed to create $CONTPATH in container."
    run_command "rmdir $tmpPath"
    exit 1
fi

if ! $(run_command "mount --bind $HOSTPATH $tmpPath"); then
    echo "Failed to bind mount from host to temp dir."
    run_command "rmdir $tmpPath"
    exit 1
fi

if ! $(run_command "mount --bind $tmpPath $CONTPATH"); then
    echo "Failed to bind mount from temp dir to container path."
    run_command "rmdir $tmpPath"
    exit 1
fi

if ! $(run_command "umount $tmpPath"); then
    echo "Warning: Failed to unmount temp dir, but continuing..."
fi

if ! $(run_command "rmdir $tmpPath"); then
    echo "Warning: Failed to remove temp dir, but continuing..."
fi

# Final check
if [ $(run_command "grep -c $CONTPATH /proc/self/mounts") -ne 0 ]; then
    echo "Successfully mounted $HOSTPATH to container PID $CONTAINER_PID at $CONTPATH"
else
    echo "Failed to mount $HOSTPATH to container PID $CONTAINER_PID at $CONTPATH"
    exit 1
fi

