#!/bin/bash -x
#

go generate
GOOS=linux GOARCH=arm go build -tags vfs
if test "$1" = "i"
then
    cmd "/c install.bat"
fi