## Explain

K8s-device-mounter Provided a service based on k8s API aggregation: `device-mounter-apiserver`.

It provides device hot mounting services for the k8s cluster in the form of an external API server.

## Api Definition

Ensure that the caller's token permission has sub resources `pods/mount` and `pods/unmount` of k8s-device-mounter.

Can bind cluster roles to target SA to obtain relevant permissions. `device-mounter.io:generate`

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: device-mounter.io:generate
rules:
  - apiGroups:
      - device-mounter.io
    resources:
      - "pods/mount"
      - "pods/unmount"
    verbs:
      - "update"
```

### Device mounting

`PUT /apis/device-mounter.io/v1alpha1/namespaces/{namespace}/pods/{name}/mount`

Headers:

| Header         | Data type | description      |
|----------------|-----------|------------------|
| Authorization  | string    | k8s user token   |

Path Param:

| Param Name  | Data type | description          |
|-------------|-----------|----------------------|
| name        | string    | Target pod name      |
| namespaces  | string    | Target pod namespace |

Query Param:

| Param Name  | Data type | description                            |
|-------------|-----------|----------------------------------------|
| device_type | string    | The type of device to be mounted       |
| container   | string    | Target container name                  |
| wait_second | integer   | Whether slave pods ready time (second) |

Body Param:
```json
{
    "resources": {   // requested k8s node resource name
        "volcano.sh/vgpu-number":"1",
        "volcano.sh/vgpu-memory":"2000"
    },
    "annotations": { // slave pod annotation
        "device-mounter.io/expansion":"true"
    }
}
```

### Device uninstallation

`PUT /apis/device-mounter.io/v1alpha1/namespaces/{namespace}/pods/{name}/unmount`

Headers:

| Header         | Data type | description      |
|----------------|-----------|------------------|
| Authorization  | string    | k8s user token   |

Path Param:

| Param Name  | Data type | description          |
|-------------|-----------|----------------------|
| name        | string    | Target pod name      |
| namespaces  | string    | Target pod namespace |

Query Param:

| Param Name  | Data type | description                                                       |
|-------------|-----------|-------------------------------------------------------------------|
| device_type | string    | The type of device to be mounted                                  |
| container   | string    | Target container name                                             |
| force       | integer   | Whether to force uninstallation (killing processes on the device) |
