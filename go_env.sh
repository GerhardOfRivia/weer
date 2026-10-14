#!/bin/bash
export _GO_VERSION=1.27.0
export _GO_OS=linux
export _GO_ARCH=amd64
export _GO_DIR=$(pwd)/.go

mkdir -p "$_GO_DIR/toolchain"
curl -fL \
  "https://go.dev/dl/go${_GO_VERSION}.${_GO_OS}-${_GO_ARCH}.tar.gz" \
  -o "$_GO_DIR/go.tar.gz"
tar -xzf "$_GO_DIR/go.tar.gz" \
  -C "$_GO_DIR/toolchain" \
  --strip-components=1

export PATH="$_GO_DIR/toolchain/bin:$PATH"
export GOPATH="$_GO_DIR/gopath"
export GOMODCACHE="$_GO_DIR/modcache"
export GOCACHE="$_GO_DIR/buildcache"

go version
go mod download