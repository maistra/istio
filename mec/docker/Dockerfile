FROM centos:8

# install latest podman 

RUN dnf -y module disable container-tools && \
    dnf -y install 'dnf-command(copr)' && \
    dnf -y copr enable rhcontainerbot/container-selinux && \
    curl -L -o /etc/yum.repos.d/devel:kubic:libcontainers:stable.repo https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/CentOS_8/devel:kubic:libcontainers:stable.repo  && \
    dnf -y install podman

# install custom config

COPY docker/containers.conf /etc/containers/
COPY docker/storage.conf /etc/containers/

# install mec

COPY mec /usr/bin/mec

VOLUME /srv

CMD mec
