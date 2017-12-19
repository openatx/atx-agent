#!/bin/bash -x
#

go generate
GOOS=linux GOARCH=arm go build -tags vfs
