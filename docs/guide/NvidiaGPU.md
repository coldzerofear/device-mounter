## Getting Started with Nvidia GPU Mounter

This document provides a brief intro of the usage of GPU Mounter.

### Prerequisite

* Install & Configure `nvidia-container-toolkit`. see [Docs](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html)
* Install nvidia device plugin. see [GitHub](https://github.com/NVIDIA/k8s-device-plugin)

### Create a Pod

NOTE:

Set environment variable `NVIDIA_VISIBLE_DEVICES`  to tell `nvidia-container-runtime` add CUDA library for the container, so we can check GPU state by `nvidia-smi` in the container. 

Ensure Pod scheduling to GPU resource nodes by setting affinity or labels.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  namespace: default
spec:
  nodeSelector:
    scheduling.device-mounter.io/nvidia_gpu: "true"
  containers:
    - name: cuda-container
      image: tensorflow/tensorflow:1.13.2-gpu
      command: ["/bin/sh"]
      args: ["-c", "while true; do echo hello; sleep 10;done"]
      env:
       - name: NVIDIA_VISIBLE_DEVICES
         value: "none"
```

### Call service

API service, see [API_Helper](API.md) 

#### 0. check GPU

```shell
$ kubectl exec -it gpu-pod -- nvidia-smi -L
No devices found.
```

#### 1. add GPU

`PUT /apis/device-mounter.io/v1alpha1/namespaces/{namespace}/pods/{name}/mount`

View the IP address of the cluster

```bash
$ kubectl cluster-info
Kubernetes control plane is running at https://{cluster-ip}:6443
CoreDNS is running at https://{cluster-ip}:6443/api/v1/namespaces/kube-system/services/kube-dns:dns/proxy
```

```shell
curl --location \
--request PUT 'https://{cluster-ip}:6443/apis/device-mounter.io/v1alpha1/namespaces/default/pods/gpu-pod/mount?device_type=NVIDIA_GPU&container=cuda-container&wait_second=30' \
--header 'Authorization: bearer token...' \
--data '{"resources": {"nvidia.com/gpu": "1"}}' 
```

check GPU state

```shell
$ kubectl exec -it gpu-pod -- nvidia-smi -L
GPU 0: Tesla V100-PCIE-32GB (UUID: GPU-f61ffc1a-9e61-1c0e-2211-4f8f252fe7bc)
```

#### 2. remove all GPU

`PUT /apis/device-mounter.io/v1alpha1/namespaces/{namespace}/pods/{name}/unmount`

```bash
curl --location \
--request POST 'https://{cluster-ip}:6443/apis/device-mounter.io/v1alpha1/namespaces/default/pods/gpu-pod/unmount?device_type=NVIDIA_GPU&container=cuda-container&force=true' \
--header 'Authorization: bearer token...' 
```

check GPU state
```shell
$ kubectl exec -it gpu-pod -- nvidia-smi -L
No devices found.
```