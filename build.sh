#!/usr/bin/env bash

VERSION=${1:-"v1.8.3"}

rm -rf build

docker run --rm -it -e VERSION=${VERSION} \
                    -v `pwd`/.compile.sh:/compile.sh \
                    -v `pwd`/dist:/build \
                    golang:1.16.2 /compile.sh
