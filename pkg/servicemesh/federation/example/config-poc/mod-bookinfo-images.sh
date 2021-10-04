#!/usr/bin/bash

cd ../../../../../samples/bookinfo/platform/kube/
for i in `grep 'image:' *.yaml | cut -d':' -f 1 ` 
do 
sed -i.x86 '/image:/s/2.1.0/2.1-z/g' $i
done
