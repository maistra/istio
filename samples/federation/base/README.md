# Service Mesh Federation Example

In this directory, you'll find a Service Mesh Federation example in which two meshes are connected to each other.

## About

One mesh control plane is installed in the `mesh1-system` Namespace by creating a `ServiceMeshControlPlane` resource named `fed-export`, the other is installed in the `mesh2-system` Namespace using a `ServiceMeshControlPlane` named `fed-import`. When using two clusters, the `fed-export` mesh is installed in the cluster pointed to by `MESH1_KUBECONFIG`, whereas the `fed-import` mesh is installed in the cluster pointed to by `MESH2_KUBECONFIG`. `ServiceMeshPeer` resources named `mesh2` and `mesh1` are created in the first mesh and the second mesh, respectively.

The script also installs two instances of the Bookinfo application in namespaces `mesh1-bookinfo` and `mesh2-bookinfo`.

The first mesh exports the `ratings` and `mongodb` Services, while the second mesh imports them.

The meshes are configured to split the `ratings` Service traffic in `mesh2-bookinfo` between
*mesh1* and *mesh2*. Furthermore, the `ratings` service in *mesh2* is configured to use the
`mongodb` service in *mesh1*.

## Running the example

Everything can be setup by running `./install.sh`. You must point the `MESH1_KUBECONFIG` and `MESH2_KUBECONFIG` environment variables to the `KUBECONFIG` files of the *mesh1* and *mesh2* cluster, respectively. To use a single cluster, point both to the same `KUBECONFIG` file.

The installation script deploys two meshes and registers each mesh in the other as a peer. The meshes are installed in a single or in two separate clusters, depending on the values of the `MESH1_KUBECONFIG` and `MESH2_KUBECONFIG` environment variables.

When the script finishes, run the following command in the *mesh1* cluster to check the connection status:

```shell
oc -n mesh1-system get servicemeshpeer mesh2 -o json | jq .status
```

Run the following command to check the connection status in *mesh2*:

```shell
oc -n mesh2-system get servicemeshpeer mesh1 -o json | jq .status
```

Check if services from *mesh1* are imported into *mesh2*:

```shell
oc -n mesh2-system get importedservicesets mesh1 -o json | jq .status
```

To see federation in action, use the bookinfo app in *mesh2* as follows:

1. Stream the logs of the `ratings` Pods in the *mesh1* cluster as follows

    ```shell
    oc logs -n mesh1-bookinfo svc/ratings -f
    ```

1. Stream the logs of the `ratings` Pods in the *mesh2* cluster:

    ```shell
    `oc logs -n mesh2-bookinfo svc/ratings -f`
    ```

1. Send many requests to the Bookinfo productpage endpoint using `siege` or repeated `curl` commands:

    ```shell
    siege http://$(oc -n mesh2-system get route istio-ingressgateway -o json | jq -r .spec.host)/productpage
    ```

1. Inspect the `ratings` Service log output to see requests hitting the Service in either the *mesh1* or the *mesh2* cluster.
