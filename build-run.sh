#!/bin/bash
#

set -ex

ADB="adb.exe"
DEST="/data/local/tmp/atx-agent"

GOOS=linux GOARCH=arm go build
$ADB push atx-agent $DEST
$ADB shell chmod 755 $DEST
$ADB shell $DEST server --stop
$ADB shell $DEST server -d
$ADB forward tcp:7912 tcp:7912
curl localhost:7912/wlan/ip
