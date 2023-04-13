FROM redhat/ubi8-micro:8.7

ARG TARGETARCH
COPY ${TARGETARCH:-amd64}/extauthz /usr/local/bin/extauthz

ENTRYPOINT ["/usr/local/bin/extauthz"]
