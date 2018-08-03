#!/bin/bash -x
#

adb.exe version

if test "$1" != "i"
then
    go generate
fi

echo "Build binary for arm ..."
GOOS=linux GOARCH=arm go build -tags vfs


adb.exe push atx-agent /data/local/tmp
adb.exe shell chmod 775 /data/local/tmp/atx-agent
adb.exe shell /data/local/tmp/atx-agent -stop
adb.exe shell /data/local/tmp/atx-agent #-nouia

#if test "$1" = "i"
#then
    #cmd "/c install.bat"
#fi
