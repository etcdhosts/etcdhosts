#!/usr/bin/env bash

VERSION=${1:-"v1.8.7"}

rm -rf build

docker run --rm -it -e VERSION=${VERSION} \
                    -v `pwd`/.compile.sh:/compile.sh \
                    -v `pwd`/dist:/build \
                    golang:1.17 /compile.sh
