#!/bin/bash
# This script is dynamic mount host volumes to a container by specifying its PID

usage() {
    echo "Usage: $0 container_pid host_path container_path"
    echo "Example: To mount /tmp/test to a container's /src with PID 12345:"
    echo "bash $0 12345 /tmp/test /src"
    exit 1
}

if [ $# -ne 3 ]; then
    usage
fi

which nsenter &>> /dev/null
if [ $? -ne 0 ]; then
    echo "Please install nsenter, command is: yum install util-linux"
    exit 1
fi

set -e

CONTAINER_PID=$1
HOSTPATH=$2
CONTPATH=$3

if ! kill -0 "$CONTAINER_PID" 2>/dev/null; then
    echo "Container PID $CONTAINER_PID not found."
    exit 1
fi

if [ ! -d $HOSTPATH ]; then
    echo "Physical $HOSTPATH does not exist!"
    exit 1
fi

REALPATH=$(readlink --canonicalize $HOSTPATH)
FILESYS=$(df -P $REALPATH | tail -n 1 | awk '{print $6}')

while read DEV MOUNT JUNK
    do
        #echo "DEV:$DEV MOUNT:$MOUNT JUNK:$JUNK"
        [ $MOUNT = $FILESYS ] && [ $DEV != "rootfs" ] && break
    done </proc/mounts
[ $MOUNT = $FILESYS ] # Sanity check!
while read A B C SUBROOT MOUNT JUNK
    do
        #echo "A:$A B:$B C:$C SUBROOT:$SUBROOT MOUNT:$MOUNT JUNK:$JUNK"
        [ $MOUNT = $FILESYS ] && break
    done < /proc/self/mountinfo
[ $MOUNT = $FILESYS ] # Moar sanity check!
SUBPATH=$(echo $REALPATH | sed s,^$FILESYS,,)
DEVDEC=$(printf "%d %d" $(stat --format "0x%t 0x%T" $DEV))

run_command="nsenter --target $CONTAINER_PID --mount --uts --ipc --net --pid -- sh -c "

if [ $($run_command "mount|grep $CONTPATH|wc -l") -ne 0 ]; then
    echo "Container with PID $CONTAINER_PID mount point $CONTPATH is already in use!"
    exit 1
fi

tmpPath="/tmpmnt_$(date +%s)"
$run_command "[ -b $DEV ] || mknod --mode 0600 $DEV b $DEVDEC"

if ! $($run_command "[ -d $tmpPath ] || mkdir -p $tmpPath"); then
    echo "Failed to create temp dir in container."
    exit 1
fi

if ! $($run_command "[ -d $CONTPATH ] || mkdir -p $CONTPATH"); then
    echo "Failed to create $CONTPATH in container."
    $run_command "rmdir $tmpPath"
    exit 1
fi

if ! $($run_command "mount $DEV $tmpPath"); then
    echo "Failed to bind mount from host to temp dir."
    $run_command "rmdir $tmpPath"
    exit 1
fi

if ! $($run_command "mount -o bind $tmpPath/$SUBROOT/$SUBPATH $CONTPATH"); then
    echo "Failed to bind mount from temp dir to container path."
    $run_command "rmdir $tmpPath"
    exit 1
fi

if ! $($run_command "umount $tmpPath"); then
    echo "Warning: Failed to unmount temp dir, but continuing..."
fi

if ! $($run_command "rmdir $tmpPath"); then
    echo "Warning: Failed to remove temp dir, but continuing..."
fi

if [ $($run_command "mount|grep $CONTPATH|wc -l") -ne 0 ]; then
    echo "Dynamic mount of physical $HOSTPATH to container PID $CONTAINER_PID at $CONTPATH is successful!"
else
    echo "Dynamic mount of physical $HOSTPATH to container PID $CONTAINER_PID at $CONTPATH failed!"
    exit 1
fi