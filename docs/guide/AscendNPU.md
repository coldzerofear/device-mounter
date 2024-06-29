## Getting Started with Ascend NPU Mounter

This document provides a brief intro of the usage of Ascend NPU Mounter.

### Prerequisite

* Install Hardware driver & firmware. see [QuickStart](https://www.hiascend.com/document/detail/zh/CANNCommunityEdition/80RC2alpha003/quickstart/quickstart/quickstart_18_0005.html)
* Install Ascend docker runtime. see [QuickStart](https://www.hiascend.com/document/detail/zh/mindx-dl/60rc1/clusterscheduling/dockerruntimeug/dlruntime_ug_007.html)
* Install Ascend Device Plugin. see [Gitee](https://gitee.com/ascend/ascend-device-plugin)

### Supported types

* Ascend 910B

### Ascend docker runtime默认挂载参考

https://www.hiascend.cn/document/detail/zh/mindx-dl/50rc1/dluserguide/clusterscheduling/dlug_installation_02_000035.html

### 为无NPU的Pod挂载NPU

创建一个没有请求NPU的Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: npu-test
  namespace: default
spec:
  containers:
  - name: default
    imagePullPolicy: IfNotPresent
    image: swr.cn-central-221.ovaijisuan.com/mindformers/mindformers1.0_mindspore2.2.11:aarch_20240125
    securityContext:
      runAsUser: 0
      runAsGroup: 0
    command: ["sh", "-c", "sleep 86400"]
    env:
      - name: LD_LIBRARY_PATH
          value: /usr/local/Ascend/driver/tools:/usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:$LD_LIBRARY_PATH
    resources:
      limits:
        cpu: 1
        memory: 200Mi
    volumeMounts:
     - name: driver
       mountPath: /usr/local/Ascend/driver
     - name: smi
       mountPath: /usr/local/bin/npu-smi
  volumes:
    - name: driver
      hostPath:
        path: /usr/local/Ascend/driver
        type: Directory
    - name: smi
      hostPath:
        path: /usr/local/bin/npu-smi
        type: File
```

> 注意：目标容器须手动挂载宿主机驱动目录，然后设置`LD_LIBRARY_PATH`环境变量，配置驱动目录，使容器驱动生效。

查看NPU状态，看不到任何设备信息

```bash
$ kubectl exec -it npu-test -- bash
$ npu-smi info
```

挂载一个NPU `device_type=ASCEND_NPU`

```bash
curl --location \
--request PUT 'https://{cluster-ip}:6443/apis/device-mounter.io/v1alpha1/namespaces/default/pods/npu-test/mount?device_type=ASCEND_NPU&wait_second=30' \
--header 'Authorization: bearer token...' \
--data '{"resources": {"huawei.com/Ascend910":"1"}}' 
```

再次检查NPU状态，可以看到已经挂载了一块NPU

```bash
$ kubectl exec -it npu-test -- bash
$ npu-smi info
+-------------------------------------------------------------------------------------------+
| npu-smi 23.0.rc1                 Version: 23.0.rc1                                        |
+----------------------+---------------+----------------------------------------------------+
| NPU   Name           | Health        | Power(W)    Temp(C)           Hugepages-Usage(page)|
| Chip                 | Bus-Id        | AICore(%)   Memory-Usage(MB)  HBM-Usage(MB)        |
+======================+===============+====================================================+
| 0     910B           | OK            | 68.4        39                0    / 0             |
| 0                    | 0000:C1:00.0  | 0           2003 / 15039      1    / 32768         |
+======================+===============+====================================================+
+----------------------+---------------+----------------------------------------------------+
| NPU     Chip         | Process id    | Process name             | Process memory(MB)      |
+======================+===============+====================================================+
| No running processes found in NPU 0                                                       |
+======================+===============+====================================================+
```