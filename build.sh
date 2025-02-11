#!/bin/bash
set -e

# Get dependencies
go mod download

# Build with optimizations
go build -ldflags="-s -w" -o jsonping

# Set capabilities to allow non-root ping
sudo setcap cap_net_raw+ep jsonping
