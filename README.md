# K8s Device Mounter

![K8s Device Mounter License](https://img.shields.io/github/license/pokerfaceSad/GPUMounter.svg)  ![GPUMounter master CI badge](https://github.com/pokerfaceSad/GPUMounter/workflows/GPUMounter-master%20CI/badge.svg)  ![GPUMounter worker CI badge](https://github.com/pokerfaceSad/GPUMounter/workflows/GPUMounter-worker%20CI/badge.svg)

K8s-Device-Mounter is a kubernetes plugin that can dynamically add or remove device resources running Pods.

<div align="center"> <img src="docs/images/SchematicDiagram.png" alt="Schematic Diagram Of Device Dynamic Mount"  /> </div>

## Features

* Supports add or remove Device resources of running Pod without stopping or restarting
* Compatible with kubernetes scheduler
* Compatible with cgroup v1 and cgroup v2

## Prerequisite 

* Kubernetes v1.22+ (other version not tested)
* Docker / Containerd (other version not tested)
* Runc based container runtime

## Supported devices

* Nvidia GPU device plugin: Support NVIDIA native device resource scheduling
> `nvidia-container-runtime` (must be configured as default runtime)

* Valcano VGPU device plugin: VGPU hot mount implementation supporting Valcano scheduler
> `nvidia-container-runtime` (must be configured as default runtime)

* Ascend NPU device plugin: Under development
> `ascend-docker-runtime` (must be configured as default runtime)


## Deploy

* label nodes with `device-mounter=enable`

```shell
kubectl label node <nodename> device-mounter-enable=enable
```

* deploy

```bash
kubectl apply -f deploy/device-mounter-apiserver.yaml
kubectl apply -f deploy/device-mounter-daemonset.yaml
```

* uninstall

```shell
kubectl delete -f deploy/device-mounter-apiserver.yaml
kubectl delete -f deploy/device-mounter-daemonset.yaml
```

* generate grpc api

```shell
 protoc --go_out=. --go-grpc_out=. pkg/api/api.proto
```

## Quick Start

See [QuickStart.md](docs/guide/QuickStart.md)

## FAQ

See  [FAQ.md](docs/guide/FAQ.md)

## License

This project is licensed under the Apache-2.0 License.

## Issues and Contributing

* Please let me know by [Issues](https://github.com/pokerfaceSad/GPUMounter/issues/new) if you experience any problems.
* [Pull requests](https://github.com/pokerfaceSad/GPUMounter/pulls) are very welcomed, if you have any ideas to make Device Mounter better.
