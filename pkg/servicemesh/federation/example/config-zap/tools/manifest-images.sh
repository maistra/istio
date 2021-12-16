#!/usr/bin/bash

for i in examples-bookinfo-mongodb \
         examples-bookinfo-details-v2 \
         examples-bookinfo-details-v1 \
         examples-bookinfo-mysqldb \
         examples-bookinfo-reviews-v2 \
         examples-bookinfo-details-v1 \
         examples-bookinfo-reviews-v1 \
         examples-bookinfo-reviews-v2 \
         examples-bookinfo-reviews-v3 \
         examples-bookinfo-productpage-v1 
do
   buildah manifest create quay.io/maistra/${i}:2.1
   for j in 2.1.0 2.1.0-ibm-p 2.1-z
   do
	   buildah manifest add \
	           quay.io/maistra/${i}:2.1 \
	           quay.io/maistra/${i}:${j}
   done
done


for i in examples-bookinfo-ratings-v2 \
         examples-bookinfo-ratings-v1 
do
   buildah manifest create quay.io/maistra/${i}:2.1
   for j in 2.1.0 2.1.0-ibm-p-2 2.1-z
   do
	   buildah manifest add \
	           quay.io/maistra/${i}:2.1 \
	           quay.io/maistra/${i}:${j}
   done
done
