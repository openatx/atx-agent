#!/bin/bash -x
#

set -e

if test "$1" != "i"
then
    go generate
fi

echo "Build binary for arm ..."
GOOS=linux GOARCH=arm go build -tags vfs

if test "$1" = "i"
then
    cmd "/c install.bat"
fi
