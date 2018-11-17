#!/bin/bash
#

set -ex

DEST="/data/local/tmp/atx-agent"

echo "Build binary for x86(emulator) ..."
GOOS=linux GOARCH=amd64 go build

# go generate
# GOOS=linux GOARCH=arm go build -tags vfs

ADB="adb"
$ADB push atx-agent $DEST
$ADB shell chmod 755 $DEST
$ADB shell $DEST server --stop
$ADB shell $DEST server -d "$@"

$ADB forward tcp:7912 tcp:7912
curl localhost:7912/wlan/ip
