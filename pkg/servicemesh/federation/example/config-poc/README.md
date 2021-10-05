# Service Mesh Federation Configuration Scripts



## Proof of Concept / Test Installation 

The prerequisites for all installations of Service Mesh federation are:

 1.  Install the Service Mesh, Kiali and Jaeger operators on the clusters; use the 'install to all namespaces' option
 2.  Set environment variables MESH1-KUBECONFIG and MESH2-KUBECONFIG: the KUBECONFIG of the clusters you intend to install federated OSSM on; 

### for OSSM Federation for libvirt IPI provisioned OCP clusters 

Libvirt and bare-metal provisioned OCP clusters on their own have no LoadBalancer service type unless you install an L4 load balancer inside each cluster in order to provide this service type.  Libvirt-provisioned clusters are for test and development purposes only; therefore, we take the quick and dirty approach to provisioning multi-cluster service mesh federation. 

This means provisioning the federation networks on NodePort services rather than the usual LoadBalancer services. We also assume that all the clusters are installed on the same host, allowing us to open up the service and discovery ports solely within the libvirt firewall zone.  


The procedure for this installation is:

 1.  run `./install-libvirt-1.sh`: this sets up the service mesh control plane, service mesh member role and bookinfo test projects.
 2.  run `./install-libvirt-2.sh`: this installs the NodePort services and opens up the appropriate firewalls in the libvirt zone
 3.  run `./install-libvirt-3.sh`: this sets up the federation plane and deploys the two bookinfo projects that will be federated in the two clusters

### Uninstaller 

No installation procedure is complete without an uninstaller, so we give you: `uninstall-libvirt.sh`

### OSSM Federation for bare metal UPI provisioned clusters:TBD 

There are two options for multi-cluster federation on bare metal and other UPI OCP installations.  One is to install a LoadBalancer service option into both clusters using e.g. "MetalLB" See https://youtu.be/8RQBt9y2xY4 for more information on this option.  

Another option is to use the NodePort service as we did in the libvirt-provisioned clusters. In this case, however, we're going to (generally) need to open up firewall ports on a variety of different hosts for whatever hosts the clusters are installed on, which could be done with some ssh scripting.   

### OSSM Federation on Openstack-provisioned IPI clusters

This section describes installation where dynamic L4 load balancer internal to the cluster in the course of IPI installation with OpenStack, which results in the LoadBalancer service type being available 

Everything for a pair of clusters which were provisioned with openstack can be setup by running `./install-openstack.sh.`

Once the install is complete, this script installs bookinfo into the following
namespaces:
  mesh1-bookinfo
  mesh2-bookinfo

```
Redirect to the aliased service through the egress gateway:

You can redirect mesh2-bookinfo to use the ratings service in mesh1-bookinfo by
modifying the mesh2-imports/ratings-aliased-mesh1 VirtualService.  Add the
following to the list of hosts:
    ratings.mesh2-bookinfo.svc.cluster.local
and modify the mesh routing (gateway=mesh) to rewrite the authority to:
    ratings-aliased.mesh1-exports.svc.cluster.local

Redirect to the actual service using passthrough on both sides:
Add the following VirtualService to mesh2.  If you've tested redirecting to the
aliased service, make sure to remove the ratings.mesh2-bookinfo.svc.cluster.local
from the hosts list in the mesh2-imports/ratings-aliased-mesh1 VirtualService.

kind: VirtualService
apiVersion: networking.istio.io/v1beta1
metadata:
  name: ratings-passthrough
  namespace: mesh2-imports
spec:
  hosts:
    - ratings.mesh2-bookinfo.svc.cluster.local
  http:
    - rewrite:
        authority: ratings.mesh1-bookinfo.svc.cluster.local
      route:
        - destination:
            host: ratings.mesh1-bookinfo.svc.cluster.local
```


## Production Installation: TBD 

Production environments will require that one be able to federate service meshes between any two (or three) clusters, regardless of whether they were UPI or IPI installs, or whether they were provisioned through any Cloud service, on-prem OpenStack, on z/VM or P or via libvirt. The network environment for each (external routers, DNS resolution, load balancers, proxies, etc) would need to be taken into account. 

Having a different install script for each possible combination would be combinatorially prohibitive.  Therefore, for production installation, ansible would be a good choice for installation of OSSM federation. 

