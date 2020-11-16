#!/usr/bin/env bash

VERSION=${1:-"v1.8.0"}

rm -rf build

docker run --rm -it -e VERSION=${VERSION} \
                    -v `pwd`/.compile.sh:/compile.sh \
                    -v `pwd`/build:/build \
                    golang:1.15 /compile.sh
