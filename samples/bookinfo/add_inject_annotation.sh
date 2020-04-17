#!/bin/bash

set -o errexit

#Add sidecar.istio.io/inject annotation
find . -name "*bookinfo*.yaml" -exec sed -i.bak 's/      \(labels:.*\)/      annotations:\
        sidecar.istio.io\/inject: "true"\
      \1/g' {} +

