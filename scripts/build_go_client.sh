#!/bin/sh
set -e
cd "$(dirname "$0")/../go_client"
# fetch dependencies and format the source
go mod download
[ -n "$(command -v gofmt)" ] && gofmt -w *.go
# build all packages
go build ./...
