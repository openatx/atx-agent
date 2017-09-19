adb forward tcp:7912 tcp:7912
curl localhost:7912/stop

adb push atx-agent /data/local/tmp
adb shell chmod 777 /data/local/tmp/atx-agent
adb shell /data/local/tmp/atx-agent -d
pause