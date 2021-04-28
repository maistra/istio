Everything can be setup by running ./install.sh.

Once the install is complete, you'll need to install bookinfo into the following
namespaces:
  mesh1-bookinfo
  mesh2-bookinfo

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

