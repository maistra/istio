# Service Mesh Federation Configuration Scripts

## Proof of Concept / Test Installation

### OSSM Multi-cluster Federation for z/VM UPI or libvirt IPI provisioned OCP clusters

#### Prerequisites

The prerequisites for all installations of Multi-Cluster Service Mesh federation are:

 1.  Install the Service Mesh, Kiali and Jaeger operators on the clusters; use the 'install to all namespaces' option
 2.  Set environment variables MESH1-KUBECONFIG and MESH2-KUBECONFIG to be the kubeconfig files for the two clusters you're going to run federated service mesh installations on, e.g.:

```
export MESH1-KUBECONFIG=./mesh1-kubeconfig
export MESH2-KUBECONFIG=./mesh2-kubeconfig
``` 

These files can be found, typically, stashed away in the `auth` directory of the cluster installation artifacts.

If you are running multi-cluster service mesh federation on bare-metal, libvirt or UPI OCP installations such as z/VM, you will (a) need to use NodePort rather than LoadBalancer service types to federate, and consequently (b) you will need to route the traffic through a proxy server such as haproxy.  

In this case:
 1.  set the PROXY_HOST environment variable if running both clusters on same host 
 1a. set   MESH1_ADDRESS and MESH2_ADDRESS if each of the two clusters are on different hosts. 
 2.  If you're using haproxy as your load balancer edit the `OPTIONS` field in `/etc/sysconfig/haproxy` file on the so it reads:

```
# Add extra options to the haproxy daemon here. This can be useful for
# specifying multiple configuration files with multiple -f options.
# See haproxy(1) for a complete list of options.
OPTIONS="-f /etc/haproxy/federation.cfg"
```

#### Procedure

The procedure for installing federation tests on two libvirt IPI installed clusters which are on the same host is:

 1.  run `./install-libvirt-1.sh`: this sets up the service mesh control plane, service mesh member role and bookinfo test projects.
 2.  run `./install-libvirt-2.sh` which:
 
     a. installs the NodePort services
     
     b. opens up the appropriate firewalls in the libvirt zone
     
     c. generates & installs the proxy configuration file `federation.cfg`
     
     d. restarts haproxy with the additional proxy configuration
     
 3.  run `./install-libvirt-3.sh`: this sets up the federation plane and deploys the two bookinfo projects that will be federated in the two clusters

If running the two clusters on two different hosts, run the install-multihost scripts instead. 

#### Test Script

run `./test-federation.sh`.  It should give you results something like:

```
./test-federation.sh

##### Using the following kubeconfig files:

mesh1: /opt/clusters-deploy/maistra-qez-49/auth/kubeconfig
mesh2: /opt/clusters-deploy/kiali-qez-49/auth/kubeconfig

##### check status of federation

##### oc1 -n mesh1-system get servicemeshpeer mesh2 -o json | jq .status:

{
  "discoveryStatus": {
    "active": [
      {
        "pod": "istiod-fed-export-59c5865775-bq9zl",
        "remotes": [
          {
            "connected": true,
            "lastConnected": "2021-10-07T04:15:52Z",
            "lastDisconnect": "2021-10-07T04:15:50Z",
            "lastEvent": "2021-10-06T23:11:40Z",
            "lastFullSync": "2021-10-07T04:16:05Z",
            "source": "10.120.2.70"
          }
        ],
        "watch": {
          "connected": true,
          "lastConnected": "2021-10-07T04:16:48Z",
          "lastDisconnect": "2021-10-07T04:16:48Z",
          "lastDisconnectStatus": "200 OK",
          "lastFullSync": "2021-10-07T04:16:48Z"
        }
      }
    ]
  }
}

##### oc2 -n mesh2-system get servicemeshpeer mesh1 -o json | jq .status:

{
  "discoveryStatus": {
    "active": [
      {
        "pod": "istiod-fed-import-58cfcc95c5-zv7br",
        "remotes": [
          {
            "connected": true,
            "lastConnected": "2021-10-07T04:16:48Z",
            "lastDisconnect": "2021-10-07T04:08:28Z",
            "lastFullSync": "2021-10-07T04:16:48Z",
            "source": "10.121.3.149"
          }
        ],
        "watch": {
          "connected": true,
          "lastConnected": "2021-10-07T04:15:52Z",
          "lastDisconnect": "2021-10-07T04:15:52Z",
          "lastDisconnectStatus": "200 OK",
          "lastFullSync": "2021-10-07T04:16:05Z"
        }
      }
    ]
  }
}

##### Check if services from mesh1 are imported into mesh2:

##### oc2 -n mesh2-system get importedservicesets mesh1 -o json | jq .status:

{
  "importedServices": [
    {
      "exportedName": "mongodb.bookinfo.svc.mesh2-exports.local",
      "localService": {
        "hostname": "mongodb.mesh2-bookinfo.svc.mesh1-imports.local",
        "name": "mongodb",
        "namespace": "mesh2-bookinfo"
      }
    },
    {
      "exportedName": "ratings.bookinfo.svc.mesh2-exports.local",
      "localService": {
        "hostname": "ratings.mesh2-bookinfo.svc.mesh1-imports.local",
        "name": "ratings",
        "namespace": "mesh2-bookinfo"
      }
    }
  ]
}

##### deploy v2 ratings system into mesh1 and mesh2

##### manual steps to test:

  1. Open http://istio-ingressgateway-mesh2-system.apps.kiali-qez-49.maistra.upshift.redhat.com/productpage
  2. Refresh the page several times and observe requests hitting either the mesh1 or the mesh2 cluster.

[root@bos-z15-l22 config-poc]# Using deprecated annotation `kubectl.kubernetes.io/default-logs-container` in pod/ratings-v2-66d7c9bb75-kzcvp. Please use `kubectl.kubernetes.io/default-container` instead
Using deprecated annotation `kubectl.kubernetes.io/default-logs-container` in pod/ratings-v2-66d7c9bb75-k2st6. Please use `kubectl.kubernetes.io/default-container` instead
Server listening on: http://0.0.0.0:9080
Server listening on: http://0.0.0.0:9080
```

#### Uninstaller

No installation procedure is complete without an uninstaller, so we give you: `uninstall-libvirt.sh`

#### Bare Metal and libvirt load balancer configuration

This part is currently incorporated in install-libvirt-2.sh except for step 3. You'll only need to edit `/etc/sysconfig/haproxy` once, so that it invokes the new `federation.cfg` haproxy configuration file.

 1. using the NodePorts mapped, generate the federation.cfg haproxy configuration file that will route packets from one cluster to another
 2. copy the federation.cfg file into /etc/sysconfig/haproxy
 3. edit the `OPTIONS` field in `/etc/sysconfig/haproxy` so it reads:
```
# Add extra options to the haproxy daemon here. This can be useful for
# specifying multiple configuration files with multiple -f options.
# See haproxy(1) for a complete list of options.
OPTIONS="-f /etc/haproxy/federation.cfg"
```
 4. restart the haproxy service:
```
systemctl restart haproxy
```

### OSSM Federation for bare metal UPI provisioned clusters:TBD

There are two options for multi-cluster federation on bare metal and other UPI OCP installations.  One is to install a LoadBalancer service option into both clusters using e.g. "MetalLB" See https://youtu.be/8RQBt9y2xY4 for more information on this option.

Another option is to use the NodePort service as we did in the libvirt-provisioned clusters. In this case, however, we're going to (generally) need to open up firewall ports on a variety of different hosts for whatever hosts the clusters are installed on, which could be done with some ssh scripting, and we will still need to do a many-to-many proxy/load balancer.


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

