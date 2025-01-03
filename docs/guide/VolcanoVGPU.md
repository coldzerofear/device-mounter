## Getting Started with Volcano vGPU Mounter

No support provided

~~This document provides a brief intro of the usage of Volcano vGPU Mounter.~~

### Prerequisite

* Install Volcano Scheduler. see [GitLab](https://git.tydic.com:11011/TRDC-CSR/cloud/mo/thirdparty/volcano/-/tree/dev)
* Install Volcano vGPU Device Plugin. see [GitLab](https://git.tydic.com:11011/TRDC-CSR/cloud/mo/thirdparty/volcano-devices/-/tree/dev)

### 为无vGPU的pod挂载vGPU

创建一个没有请求vGPU的Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  namespace: default
spec:
  schedulerName: volcano
  nodeSelector:
    scheduling.device-mounter.io/ascend_vgpu: "true"
  containers:
    - name: ubuntu-container
      command: ["sh", "-c", "sleep 86400"]
      image: registry.tydic.com/cube-studio/gpu-player:v2
      env:
        - name: NVIDIA_VISIBLE_DEVICES
          value: "none"
      resources:
        limits:
          cpu: 1
          memory: 200Mi
```

> 注意：目标容器须设置`NVIDIA_VISIBLE_DEVICES=none`环境变量，让nvidia-runtime为容器注入相关驱动，否则热挂载将失败。

查看GPU状态

```bash
$ kubectl exec -it gpu-pod -- nvidia-smi -L
No devices found.
```

挂载一个vGPU，可以限制显存大小

```bash
curl --location \
--request PUT 'https://{cluster-ip}:6443/apis/device-mounter.io/v1alpha1/namespaces/default/pods/gpu-pod/mount?device_type=VOLCANO_VGPU&container=ubuntu-container&wait_second=30' \
--header 'Authorization: bearer token...' \
--data '{"resources": {"volcano.sh/vgpu-number": "1","volcano.sh/vgpu-memory": "1024"}}' 
```

查看GPU状态

```bash
$ kubectl exec -it gpu-pod -- nvidia-smi  
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 550.54.15              Driver Version: 550.54.15      CUDA Version: 12.4     |
|-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GeForce RTX 3080 Ti     Off |   00000000:09:00.0 Off |                  N/A |
| 30%   31C    P8              6W /  350W |       0MiB /   1024MiB |      0%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+
                                                                                         
+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI        PID   Type   Process name                              GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
+-----------------------------------------------------------------------------------------+
```

卸载vGPU

```bash
curl --location \
--request POST 'https://{cluster-ip}:6443/apis/device-mounter.io/v1alpha1/namespaces/default/pods/gpu-pod/unmount?device_type=VOLCANO_VGPU&container=ubuntu-container&force=true' \
--header 'Authorization: bearer token...' 
```

再次查看GPU状态，已成功卸载

```bash
$ kubectl exec -it gpu-pod -- nvidia-smi -L
No devices found.
```

### 为有vGPU的pod扩容显存

创建一个请求了vGPU的Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  namespace: default
spec:
  schedulerName: volcano
  nodeSelector:
    scheduling.device-mounter.io/ascend_vgpu: "true"
  containers:
    - name: ubuntu-container
      command: ["sh", "-c", "sleep 86400"]
      image: registry.tydic.com/cube-studio/gpu-player:v2
      resources:
        limits:
          cpu: 1
          memory: 200Mi
          volcano.sh/vgpu-number: 1
          volcano.sh/vgpu-memory: 1024
```

查看GPU状态

```bash
$ kubectl exec -it gpu-pod -- nvidia-smi  
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 550.54.15              Driver Version: 550.54.15      CUDA Version: 12.4     |
|-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GeForce RTX 3080 Ti     Off |   00000000:09:00.0 Off |                  N/A |
| 30%   31C    P8              6W /  350W |       0MiB /   1024MiB |      0%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+
                                                                                         
+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI        PID   Type   Process name                              GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
+-----------------------------------------------------------------------------------------+
```

扩容vGPU

```bash
curl --location \
--request PUT 'https://{cluster-ip}:6443/apis/device-mounter.io/v1alpha1/namespaces/default/pods/gpu-pod/mount?device_type=VOLCANO_VGPU&container=ubuntu-container&wait_second=30' \
--header 'Authorization: bearer token...' \
--data '{"resources": {"volcano.sh/vgpu-number": "1","volcano.sh/vgpu-memory": "1024"}, "annotation": {"device-mounter.io/expansion":"true"}}' 
```

检查vGPU状态，已扩容成功

```bash
 kubectl exec -it gpu-pod -- nvidia-smi 
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 550.54.15              Driver Version: 550.54.15      CUDA Version: 12.4     |
|-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GeForce RTX 3080 Ti     Off |   00000000:09:00.0 Off |                  N/A |
| 30%   33C    P8              7W /  350W |       0MiB /   2048MiB |      0%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+
                                                                                         
+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI        PID   Type   Process name                              GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
+-----------------------------------------------------------------------------------------+
```

> 注意: 只能扩容Pod创建时申请的vGPU，且扩容的vGPU无法缩容。 vGPU-Mounter只能卸载热挂载的vGPU，无法卸载Pod创建时申请的vGPU。