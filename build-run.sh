#!/bin/bash
#


ADB=$(which adb.exe) # for windows-linux

set -ex
ADB=${ADB:-"adb"}

DEST="/data/local/tmp/atx-agent"

echo "Build binary for arm ..."
#GOOS=linux GOARCH=arm go build

go generate
GOOS=linux GOARCH=arm go build -tags vfs

$ADB push atx-agent $DEST
$ADB shell chmod 755 $DEST
$ADB shell $DEST server --stop
# $ADB shell $DEST server -d "$@"

$ADB forward tcp:7912 tcp:7912
$ADB shell $DEST server

