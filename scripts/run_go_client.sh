#!/bin/sh
set -e
cd "$(dirname "$0")/../go_client"
exec go run . "$@"
