#!/bin/bash
#

go generate
GOOS=linux GOARCH=arm go build -tags vfs
