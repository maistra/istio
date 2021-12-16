# OSSM-federation-zap-config

OpenShift Service Mesh federation configuration for IBM Systems P 

derived from github.com/maistra/istio/pkg/servicemesh/federation/example/config-poc


## Service Mesh Federation Configuration Test Scripts

### Overview

OSSM Federation connects groups of microservices managed under two different service mesh control planes. 

The practical use case of this is to do more advanced load sharing and management on e.g. two geographically separated clusters.  

Never the less, for development and test purposes, it makes sense to configure your two flights of micro-services on the same cluster initially, as this case is the simplest to configure the environment for. 

Configuring two clusters' environments to support OSSM federation between them is also relatively simple in cloud environments, as you can use the LoadBalancer service type to route your applications' data within the federated data plane, and routing between the clusters is handled by the cloud provider's load balancer.  

However, in clusters provisioned on 'bare metal' systems, and virtualized on IBM System Z and Power systems, you need to use the NodePort service type to route your applications' data within the federated data planes, but between the nodes of the clusters.  This means you need to make sure to open the necessary firewall ports, and make sure data can be routed between the nodes of one cluster, to the nodes of the other.  This _can_ mean taking a good hard look at your DNS settings, and provisioning a load balancer in your environment, external to the two clusters. The example scripts for these cases assume you are using `haproxy` for the load balancer and `firewalld` for your firewall; for deployment in production environments, please feel free to use these scripts as examples of what you'll need to do in your own environments.     

### Prerequisites

The prerequisites for all installations of OSSM federation are:

 1.  Install the Service Mesh, Kiali and Jaeger operators on the clusters; use the 'install to all namespaces' option
 2.  Set environment variables MESH1-KUBECONFIG and MESH2-KUBECONFIG to be the kubeconfig files for the two clusters you're going to run federated service mesh installations on. These files can be found, typically, stashed away in the `auth` directory of the cluster installation artifacts.
e.g.:

```
export MESH1_KUBECONFIG=/root/ipi/pipe-2/auth/kubeconfig
export MESH2_KUBECONFIG=/root/second_cluster/kubeconfig

Note: For different hosts, copy kubeconfig of second cluster on the host having first cluster using scp command at path "/root/second_cluster/kubeconfig"
```
 3.  Run the ./tools/mod-bookinfo-images.sh to update the bookinfo images. 

### "Single Cluster" federation

If you are testing federated services on two different service meshes on the same cluster, you can use the `install_onecluster.sh` script without any further system configuration.


### Prerequisites for running OSSM federation on "multi-cluster"

If you are running multi-cluster service mesh federation on bare-metal, libvirt or UPI OCP installations such as z/VM, you will (a) need to use NodePort rather than LoadBalancer service types to federate, and consequently (b) you will need to route the traffic somehow.  Here we use a proxy server -- haproxy.    

To install `haproxy` on RHEL 8, `sudo yum install -y haproxy`

In this case:
 1a.  set the PROXY_HOST environment variable if running both clusters on same host 
 1b. set   MESH1_ADDRESS and MESH2_ADDRESS if each of the two clusters are on different hosts. 
 2.  Edit the `OPTIONS` field in `/etc/sysconfig/haproxy` file so it reads on single/both hosts:

```# Add extra options to the haproxy daemon here. This can be useful for
# specifying multiple configuration files with multiple -f options.
# See haproxy(1) for a complete list of options.
OPTIONS="-f /etc/haproxy/federation.cfg"
```

### OSSM Multi-cluster Federation on two libvirt IPI OCP clusters provisioned on the same host

1. Run ./install-libvirt.sh
	a. This script creates mesh1-system,mesh1-bookinfo on cluster1 and mesh2-system,mesh2-bookinfo on cluster2.
	   Also, creates smcp and smmr in respective mesh1-system and mesh2-system namespaces.
	b. Installs the NodePort services
	c. Opens the appropriate firewalls in the libvirt zone
	d. Generates & installs the proxy configuration file federation.cfg
	e. Restarts haproxy with the additional proxy configuration
	f. sets up the federation plane 
	g. deploys the two bookinfo projects that will be federated in the two clusters

2. Check the federation configuration is successfully copied to location /etc/haproxy/
```
	# cat /etc/haproxy/federation.cfg
```

3. Check the firewall is opened for discovery and service ports
```	
	# grep -w '<discovery_port/service_port>/tcp' /etc/services
	e.g. # grep -w '32568/tcp' /etc/services
	Note: If firewall is not opened, manually copy front end and backend sections from federation.cfg to /etc/haproxy/haproxy 
	and restart haproxy service
```	
	
4. Check the two bookinfo projects, are up and running
```
	# oc get pods -n mesh1-bookinfo
	# oc get pods -n mesh2-bookinfo
```


### OSSM Multi-cluster Federation provisioned on two different hosts, either IPI libvirt, PowerVM or z/VM, or bare metal installs

1. Exchange public keys using ssh-copy-id so the host you're running the install scripts from can ssh into the other host without having to enter a password 
```
	# ssh-copy-id -i ~/.ssh/id_rsa.pub root@<host_ip>
```
2. Run the `install-multihost.sh` script instead of `./install-libvirt.sh` and check the results for respective cluster on respective host as mentioned above from step 2.

### Test Script

./tools/test-federation.sh is the script which gives the federation status, services imported from mesh1 to mesh2 and deploy v2 ratings system into mesh1 and mesh2

Manual test:
To check federation manually:
1. To check the status of federation
```
# oc -n mesh1-system get servicemeshpeer mesh2 -o json | jq .status # on cluster1
# oc -n mesh2-system get servicemeshpeer mesh1 -o json | jq .status # on cluster2
```

2. To check services imported from mesh1 into mesh2
```
# oc -n mesh2-system get importedservicesets mesh1 -o json | jq .status # on cluster2
```
3. To see federation in action, using the bookinfo app in mesh2
1. On mesh1 cluster, run: oc logs -n mesh1-bookinfo deploy/ratings-v2-mysql -f
2. On mesh2 cluster: oc logs -n mesh2-bookinfo deploy/ratings-v2-mysql -f
3. Open http://$(oc -n mesh2-system get route istio-ingressgateway -o json | jq -r .spec.host)/productpage
4. Refresh the page several times and observe requests hitting either the mesh1 or the mesh2 cluster.


### Uninstallers

the script `tools/uninstall-federation.sh` can be run to delete all of the federation test artifacts. 




