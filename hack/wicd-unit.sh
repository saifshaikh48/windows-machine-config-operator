#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail


WICD_UNIT_EXE="wicd_unit.exe"
# Build unit tests for Windows. Specifically targetting packages used by WICD.
for dir in ./pkg/daemon/*/;do
    package=$(basename $dir)
    GOOS=windows GOFLAGS=-v go test -c $dir... -o ${package}_$WICD_UNIT_EXE
done
