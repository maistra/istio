# OSSM-federation-zap-config

OpenShift Service Mesh federation configuration for IBM Systems P

derived from github.com/maistra/istio/pkg/servicemesh/federation/example/config-poc

## Service Mesh Federation Configuration Test Scripts

### Overview

OSSM Federation connects groups of microservices managed under two
different service mesh control planes.

The practical use case of this is to do more advanced load sharing
and management on e.g. two geographically separated clusters.

Never the less, for development and test purposes, it makes
sense to configure your two flights of micro-services on the same
cluster initially, as this case is the simplest to configure the
environment for.

Configuring two clusters' environments to support OSSM federation between them
is also relatively simple in cloud environments, as you can use the
LoadBalancer service type to route your applications' data within the
federated data plane, and routing between the clusters is handled by the
cloud provider's load balancer.

However, in clusters provisioned on 'bare metal' systems, and virtualized
on IBM System Z and Power systems, you need to use the NodePort service
type to route your applications' data within the federated data planes,
but between the nodes of the clusters.  This means you need to make sure
to open the necessary firewall ports, and make sure data can be routed
between the nodes of one cluster, to the nodes of the other.

This _can_ mean taking a good hard look at your DNS settings, and
provisioning a load balancer in your environment, external to the
two clusters. The example scripts for these cases assume you are
using `haproxy` for the load balancer and `firewalld` for your firewall;
for deployment in production environments, please feel free to use these
scripts as examples of what you'll need to do in your own environments.

### Prerequisites

The prerequisites for all installations of OSSM federation are:

- Install the Service Mesh, Kiali and Jaeger operators on the clusters
- Use the 'install to all namespaces' option
- Set environment variables MESH1-KUBECONFIG and MESH2-KUBECONFIG.

These are the kubeconfig files for the
two clusters you're going to run federated service mesh
installations on. These files can be found, typically,
stashed away in the `auth` directory of the cluster installation artifacts.
e.g.:

```bash
export MESH1_KUBECONFIG=/root/ipi/pipe-2/auth/kubeconfig
export MESH2_KUBECONFIG=/root/second_cluster/kubeconfig
```

- Run the ./tools/mod-bookinfo-images.sh to update the bookinfo images.

Note: For different hosts, copy kubeconfig of second cluster on the host having
first cluster using scp command at path "/root/second_cluster/kubeconfig"

### "Single Cluster" federation

If you are testing federated services on two different service meshes on the
same cluster, you can use the `install_onecluster.sh` script without any
further system configuration.

### Prerequisites for running OSSM federation on "multi-cluster"

If you are running multi-cluster service mesh federation on bare-metal,
libvirt or UPI OCP installations such as z/VM, you will (a) need to use
NodePort rather than LoadBalancer service types to federate, and
consequently (b) you will need to route the traffic somehow.

Here we use a proxy server -- haproxy.

To install `haproxy` on RHEL 8, `sudo yum install -y haproxy`

In this case:

- set the PROXY_HOST environment variable if both clusters on same host
- set `MESH1_ADDRESS` and `MESH2_ADDRESS` two clusters are on different hosts.
- Edit the `OPTIONS` field in `/etc/sysconfig/haproxy` file:

```bash
# Add extra options to the haproxy daemon here. This can be useful for
# specifying multiple configuration files with multiple -f options.
# See haproxy(1) for a complete list of options.
OPTIONS="-f /etc/haproxy/federation.cfg"
```

### Multi-cluster Federation on two libvirt IPI OCP clusters on the same host

- Run ./install-libvirt.sh
       - This script creates:
             - mesh1-system
             - mesh1-bookinfo on cluster1
             - mesh2-system,mesh2-bookinfo on cluster2.
       - creates smcp and smmr in mesh1-system and mesh2-system namespaces.
       - Installs the NodePort services
       - Opens the appropriate firewalls in the libvirt zone
       - Generates & installs the proxy configuration file federation.cfg
       - Restarts haproxy with the additional proxy configuration
       - sets up the federation plane
       - deploys the two bookinfo projects that will be federated in the two clusters

- Check the federation configuration is successfully copied to location /etc/haproxy/

```bash
   # cat /etc/haproxy/federation.cfg
```

- Check the firewall is opened for discovery and service ports

```bash
# grep -w '<discovery_port/service_port>/tcp' /etc/services
e.g. # grep -w '32568/tcp' /etc/services
Note: If firewall is not opened, manually copy front end and
backend sections from federation.cfg to /etc/haproxy/haproxy
and restart haproxy service
```

- Check the two bookinfo projects, are up and running

```bash
# oc get pods -n mesh1-bookinfo
# oc get pods -n mesh2-bookinfo
```

### OSSM Multi-cluster Federation provisioned on two different hosts

This can apply to any of: IPI libvirt, PowerVM or z/VM, or bare metal installs

- Exchange public keys using ssh-copy-id

```bash
# ssh-copy-id -i ~/.ssh/id_rsa.pub root@<host_ip>
```

- passwordless ssh from the host you're running the install scripts
- Run the `install-multihost.sh` script instead of `./install-libvirt.sh`
- check the results for respective cluster on respective host as in step 2.

### Test Script

`./tools/test-federation.sh` is the script which gives the federation status,
services imported from mesh1 to mesh2 and deploy v2 ratings system into
mesh1 and mesh2.

#### Manual test

To check federation manually:

- To check the status of federation

```bash
# oc -n mesh1-system get servicemeshpeer mesh2 -o json | jq .status # on cluster1
# oc -n mesh2-system get servicemeshpeer mesh1 -o json | jq .status # on cluster2
```

- To check services imported from mesh1 into mesh2

```bash
# oc -n mesh2-system get importedservicesets mesh1 -o json | jq .status # on cluster2
```

#### To see federation in action, using the bookinfo app in mesh2

- On mesh1 cluster, run: oc logs -n mesh1-bookinfo deploy/ratings-v2-mysql -f
- On mesh2 cluster: oc logs -n mesh2-bookinfo deploy/ratings-v2-mysql -f
- Open

```bash
http://$(oc -n mesh2-system get route istio-ingressgateway -o json | jq -r .spec.host)/productpage
```

- Refresh the page several times
- observe requests hitting either the mesh1 or the mesh2 cluster.

### Uninstallers

The script `tools/uninstall-federation.sh` can be run to delete all
of the federation test artifacts.
30
