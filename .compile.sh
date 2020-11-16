#!/usr/bin/env bash

set -ex

apt update \
  && apt upgrade -y \
  && apt install git -y

# clone source
mkdir -p ${GOPATH}/src/github.com/coredns ${GOPATH}/src/github.com/ytpay
git clone https://github.com/coredns/coredns.git ${GOPATH}/src/github.com/coredns/coredns
git clone https://github.com/ytpay/etcdhosts.git ${GOPATH}/src/github.com/ytpay/etcdhosts

# copy plugin
mkdir -p ${GOPATH}/src/github.com/coredns/coredns/plugin/etcdhosts
cp ${GOPATH}/src/github.com/ytpay/etcdhosts/*.go ${GOPATH}/src/github.com/coredns/coredns/plugin/etcdhosts

# make
cd ${GOPATH}/src/github.com/coredns/coredns
git checkout tags/${VERSION} -b ${VERSION}
sed -i '/^hosts:hosts/i\etcdhosts:etcdhosts' plugin.cfg
make -f Makefile gen
make -f Makefile.release release DOCKER=coredns

