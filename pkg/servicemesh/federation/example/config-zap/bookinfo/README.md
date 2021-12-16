# Bookinfo

These are the deployment files for installing bookinfo test images
into a service mesh
The images referred to in these yamls are the multi-arch manifests
in maistra, tagged 2.1
This allows users of the Z and P platforms to run the example/test
installations of federation without needing to go in and modify
the yaml files for the image tags every time.

## platform/kube/

These set up the various bookinfo images into the service mesh.

## networking

Sets up various istio gateway options & networking among the
various bookinfo microservices.
