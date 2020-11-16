set -ex

apt update \
  && apt upgrade -y \
  && apt install git -y

# clone source
mkdir -p ${GOPATH}/src/github.com/coredns ${GOPATH}/src/github.com/ytpay
git clone https://github.com/coredns/coredns.git ${GOPATH}/src/github.com/coredns/coredns

# make
cd ${GOPATH}/src/github.com/coredns/coredns
git checkout tags/${VERSION} -b ${VERSION}
go get github.com/ytpay/etcdhosts@${VERSION}
sed -i '/^hosts:hosts/i\etcdhosts:github.com/ytpay/etcdhosts' plugin.cfg
make -f Makefile.release build tar DOCKER=coredns

mv release/* /build

#!/usr/bin/env bash

set -ex

apt update \
  && apt upgrade -y \
  && apt install git -y

# clone source
mkdir -p ${GOPATH}/src/github.com/coredns ${GOPATH}/src/github.com/ytpay
git clone https://github.com/coredns/coredns.git ${GOPATH}/src/github.com/coredns/coredns

# make
cd ${GOPATH}/src/github.com/coredns/coredns
git checkout tags/${VERSION} -b ${VERSION}
go get github.com/ytpay/etcdhosts@${VERSION}
sed -i '/^hosts:hosts/i\etcdhosts:github.com/ytpay/etcdhosts' plugin.cfg
make -f Makefile.release build tar DOCKER=coredns

mv release/* /build
