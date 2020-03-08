#!/bin/bash
#


ADB=$(which adb.exe) # for windows-linux

set -ex
ADB=${ADB:-"adb"}

DEST="/data/local/tmp/atx-agent"

echo "Build binary for arm ..."
ABI=$(adb shell getprop ro.product.cpu.abi)

GOARCH=
case "$ABI" in
	arm64-v8a)
		GOARCH=arm64
		;;
	*)
		GOARCH=arm
		;;
esac

#GOOS=linux GOARCH=$GOARCH go build

go generate
GOOS=linux GOARCH=$GOARCH go build -tags vfs

$ADB push atx-agent $DEST
$ADB shell chmod 755 $DEST
$ADB shell $DEST server --stop

$ADB forward tcp:7912 tcp:7912

# start server
$ADB shell $DEST server "$@"
