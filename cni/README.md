# Istio CNI plugin

For application pods in the Istio service mesh, all traffic to/from the pods needs to go through the
sidecar proxies (istio-proxy containers).  This `istio-cni` Container Network Interface (CNI) plugin will
set up the pods' networking to fulfill this requirement in place of the current Istio injected pod `initContainers`
`istio-init` approach.

This is currently accomplished via configuring the iptables rules in the netns for the pods.

The CNI handling the netns setup replaces the current Istio approach using a `NET_ADMIN` privileged
`initContainers` container, `istio-init`, injected in the pods along with `istio-proxy` sidecars.  This
removes the need for a privileged, `NET_ADMIN` container in the Istio users' application pods.

## Ambient mode

In addition to configuring application pods, if Ambient mode is enabled, the `istio-cni` plugin also configures the node-level
proxy (ztunnel):

- Sets up redirection on the node such that traffic from application pods is forwarded to ztunnel, in the host/node network namespace.
- Configures iptables and packet routing miscellanea within the `ztunnel` network namespace.

Ambient mode may also operate in the container namespace, including cases where ztunnel is run as a sidecar if the
native CNI does not support host interception. This is treated as "sidecar interception" even if it runs ztunnel.

### Ambient redirection mode

If Ambient mode is enabled, the CNI plugin currently supports two different mechanisms for Ambient traffic redirection.

1. `iptables` and `geneve` tunnels
1. `eBPF` programs and maps

These are not mutually compatible, and one or the other will be used, depending on the `CNIAmbientConfig.redirectMode` flag. The current default is `iptables`+`geneve`, though that is expected to change.

## Usage

A complete set of instructions on how to use and install the Istio CNI is available on the Istio documentation site under [Install Istio with the Istio CNI plugin](https://istio.io/latest/docs/setup/additional-setup/cni/).

## Troubleshooting

### Validate the iptables are modified

1. Collect your pod's container id using kubectl.

    ```console
    $ ns=test-istio
    $ podnm=reviews-v1-6b7f6db5c5-59jhf
    $ container_id=$(kubectl get pod -n ${ns} ${podnm} -o jsonpath="{.status.containerStatuses[?(@.name=='istio-proxy')].containerID}" | sed -n 's/docker:\/\/\(.*\)/\1/p')
    ```

1. SSH into the Kubernetes worker node that runs your pod.

1. Use `nsenter` (or `ip netns exec`) to view the iptables.

    ```console
    $ cpid=$(docker inspect --format '{{ .State.Pid }}' $container_id)
    $ nsenter -t $cpid -n iptables -L -t nat -n -v --line-numbers -x
    ```

### Collecting Logs

#### Using `istioctl`/helm

- Set: `values.global.logging.level="cni:debug,ambient:debug"`
- Inspect the pod logs of a `istio-cni` Daemonset pod on a specific node.

#### From a specific node syslog

The CNI plugins are executed by threads in the `kubelet` process.  The CNI plugins logs end up the syslog
under the `kubelet` process. On systems with `journalctl` the following is an example command line
to view the last 1000 `kubelet` logs via the `less` utility to allow for `vi`-style searching:

```console
$ journalctl -t kubelet -n 1000 | less
```

#### GKE via Stackdriver Log Viewer

Each GKE cluster's will have many categories of logs collected by Stackdriver.  Logs can be monitored via
the project's [log viewer](https://cloud.google.com/logging/docs/view/overview) and/or the `gcloud logging read`
capability.

The following example grabs the last 10 `kubelet` logs containing the string "cmdAdd" in the log message.

```console
$ gcloud logging read "resource.type=k8s_node AND jsonPayload.SYSLOG_IDENTIFIER=kubelet AND jsonPayload.MESSAGE:cmdAdd" --limit 10 --format json
```

## API

Istio CNI injection is currently based on the same Pod annotations used in init-container/inject mode.

TODO: list all supported annotations, which are working on ambient, plans for long-term support as CRD/broader than Istio.

### Selection API

- plugin config "exclude namespaces" applies first
- ambient is enabled if:
    - namespace label "istio.io/dataplane-mode" == "ambient" is required (may change for 'on-by-default' mode)
    - "sidecar.istio.io/status" annotation is not present on the pod (created by injection of sidecar)
    - "ambient.istio.io/redirection" is not "disabled"
- sidecar interception is enabled if:
    - "istio-init" container is not present in the pod.
    - istio-proxy container exists and
        - does not have DISABLE_ENVOY environment variable (which triggers proxyless mode)
        - has a istio-proxy container, with first 2 args "proxy" and "sidecar" - or less then 2 args, or first arg not proxy.
        - "sidecar.istio.io/inject" is not false
        - "sidecar.istio.io/status" exists

### Redirect API

The annotation based control is currently only supported in 'sidecar' mode. See plugin/redirect.go for details.

- redirectMode allows TPROXY may to be set, required envoy has extra permissions. Default is redirect.
- includeIPCidr, excludeIPCidr
- includeInboudPorts, excludeInboundPorts
- includeOutboutPorts, excludeOutboundPorts
- excludeInterfaces
- kubevirtInterfaces
- ISTIO_META_DNS_CAPTURE env variable on the proxy - enables dns redirect
- INVALID_DROP env var on proxy - changes behavior from reset to drop in iptables
- auto excluded inbound ports: 15020, 15021, 15090

The code automatically detects the proxyUID and proxyGID from RunAsUser/RunAsGroup and exclude them from interception, defaulting to 1337

## Implementation Details

### Overview

- [istio-cni Helm chart](../manifests/charts/istio-cni/templates)
    - `install-cni` daemonset - main function is to install and help the node CNI, but it is also a proper server and interacts with K8S, watching Pods for recovery.
    - `istio-cni-config` configmap with CNI plugin config to add to CNI plugin chained config
    - creates service-account `istio-cni` with `ClusterRoleBinding` to allow gets on pods' info and delete/modifications for recovery.

- `install-cni` container
    - copies `istio-cni` and `istio-iptables` to `/opt/cni/bin`
    - creates kubeconfig for the service account the pod runs under
    - periodically copy the K8S JWT token for istio-cni on the host to connect to K8S.
    - injects the CNI plugin config to the CNI config file
        - CNI installer will try to look for the config file under the mounted CNI net dir based on file name extensions (`.conf`, `.conflist`)
        - the file name can be explicitly set by `CNI_CONF_NAME` env var
        - the program inserts `CNI_NETWORK_CONFIG` into the `plugins` list in `/etc/cni/net.d/${CNI_CONF_NAME}`
    - the actual code is in pkg/install - including a readiness probe, monitoring.
    - it also sets up a UDS socket for istio-cni to send logs to this container.
    - based on config, it may run the 'repair' controller that detects pods where istio setup fails and restarts them, or created in corner cases.
    - if ambient is enabled, also runs an ambient controller, watching Pod, Namespace

- `istio-cni`
    - CNI plugin executable copied to `/opt/cni/bin`
    - currently implemented for k8s only
    - on pod add, determines whether pod should have netns setup to redirect to Istio proxy. See [cmdAdd](#cmdadd-workflow) for detailed logic.
        - it connects to K8S using the kubeconfig and JWT token copied from install-cni to get Pod and Namespace. Since this is a short-running command, each invocation creates a new connection.
        - If so, calls `istio-iptables` with params to setup pod netns
        - If ambient, sets up the ambient logic.

- `istio-iptables`
    - sets up iptables to redirect a list of ports to the port envoy will listen
    - shared code with istio-init container
    - it will generate an iptables-save config, based on annotations/labels and other settings, and apply it.

### CmdAdd Workflow

`CmdAdd` is triggered when there is a new pod created. This runs on the node, in a chain of CNI plugins - Istio is
run after the main CNI sets up the pod IP and networking.

1. Check k8s pod namespace against exclusion list (plugin config)
    - Config must exclude namespace that Istio control-plane is installed in (TODO: this may change, exclude at pod level is sufficient and we may want Istiod and other istio components to use ambient too)
    - If excluded, ignore the pod and return prevResult
1. Setup redirect rules for the pods:
    - Get the port list from pods definition, as well as annotations.
    - Setup iptables with required port list: `nsenter --net=<k8s pod netns> /opt/cni/bin/istio-iptables ...`. Following conditions will prevent the redirect rules to be setup in the pods:
        - Pods have annotation `sidecar.istio.io/inject` set to `false` or has no key `sidecar.istio.io/status` in annotations
        - Pod has `istio-init` initContainer - this indicates a pod running its own injection setup.
1. Return prevResult

## Reference

The framework for this implementation of the CNI plugin is based on the
[containernetworking sample plugin](https://github.com/containernetworking/plugins/tree/main/plugins/sample)

The details for the deployment & installation of this plugin were pretty much lifted directly from the
[Calico CNI plugin](https://github.com/projectcalico/cni-plugin).

Specifically:

- The CNI installation script is containerized and deployed as a daemonset in k8s.  The relevant calico k8s manifests were used as the model for the istio-cni plugin's manifest:
    - [daemonset and configmap](https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/hosted/calico.yaml) - search for the `calico-node` Daemonset and its `install-cni` container deployment
    - [RBAC](https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/rbac.yaml) - this creates the service account the CNI plugin is configured to use to access the kube-api-server

The installation program `install-cni` injects the `istio-cni` plugin config at the end of the CNI plugin chain
config.  It creates or modifies the file from the configmap created by the Kubernetes manifest.

## TODO

- Watch configmaps or CRDs and update the `istio-cni` plugin's config with these options.
