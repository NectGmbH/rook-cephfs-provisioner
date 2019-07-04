# rook-cephfs-provisioner
Kubernetes controller for automatic provisioning of PVCs using a rook shared filesystem as PV backend.

The provisioner works by sharing one cephfs and creating individual directories for each PVC. Using the rook flexvolume plugin the individual folders gets then mounted into the pod.

**IMPORTANT:** rook-cephfs-provisioner does **NOT** add any seperation except individual mountpoints. Be sure to validate your usecase with that security limitation. (PRs to fix that are welcome anyways)

## Status

Basic functionality is implemented and it's already used in testing scenarios, production-grade testing and unittests are missing yet, so use with caution.

## Deploy using helm
```
$ helm upgrade -i rook-cephfs-provisioner --namespace rook-cephfs-provisioner ./chart -f my-values.yaml
```

### Values

| Key                            | Default value                             | Description                                                                           |
| ------------------------------ | ----------------------------------------- | ------------------------------------------------------------------------------------- |
| image                          | 'kavatech/rook-cephfs-provisioner:v0.1.3' | Image of the container                                                                |
| storageClassName               | 'rook-cephfs'                             | Name of the storageclass which will be created for accessing the shared filesystem.   |
| fsName                         | ''                                        | Name of the (already existing) rook shared filesystem which should be used.           |
| clusterNamespace               | 'rook-ceph'                               | Name of the k8s namespace where the rook cluster is located.                          |
| rbac.enabled                   | true                                      | Whether RBAC should be used and roles/bindings/sa deployed.                           |
| verbose                        | false                                     | Whether verbose logging should be used.                                               |

## Acknowledgements

This project is kindly sponsored by [Nect](https://nect.com)

## License

Licensed under [MIT](./LICENSE).