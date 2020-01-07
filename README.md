# atx-agent
[![Build Status](https://travis-ci.org/openatx/atx-agent.svg?branch=master)](https://travis-ci.org/openatx/atx-agent)

这个项目的主要目的是为了屏蔽不同安卓机器的差异，然后开放出统一的HTTP接口供 [openatx/uiautomator2](https://github.com/openatx/uiautomator2)使用。项目最终会发布成一个二进制程序，运行在Android系统的后台。

这个项目是如何屏蔽不同机器的差异的呢？举个例子来说，截图这个操作，大概需要3次判断才行。

1. 先判断minicap是否安装可用，然后minicap截图。毕竟minicap截图速度最快
2. 使用uiautomator2提供的接口截图。（模拟器除外）
3. 使用screencap截图，然后根据屏幕的旋转调整旋转方向。(一般只有模拟器用这种方式截图)

正是Android手机不同的表现形式，才导致了需要这么多的判断。而atx-agent就是为了将这些操作帮你处理了。然后提供统一的HTTP接口(GET /screenshot)供你使用。

# Develop
这个项目是用Go语言写成的。编译的时候的需要你有一点Go语言的基础。
更多内容查看 [DEVELOP.md](DEVELOP.md)

# Installation
从<https://github.com/openatx/atx-agent/releases>下载以`linux_armv7.tar.gz`结尾的二进制包。绝大部分手机都是linux-arm架构的。

解压出`atx-agent`文件，然后打开控制台
```bash
$ adb push atx-agent /data/local/tmp
$ adb shell chmod 755 /data/local/tmp/atx-agent
# launch atx-agent in daemon mode
$ adb shell /data/local/tmp/atx-agent server -d

# stop already running atx-agent and start daemon
$ adb shell /data/local/tmp/atx-agent server -d --stop
```

默认监听的端口是7912。

# 常用接口
假设手机的地址是$DEVICE_URL (eg: `http://10.0.0.1:7912`)

## 获取手机截图
```bash
# jpeg format image
$ curl $DEVICE_URL/screenshot

# 使用内置的uiautomator截图
$ curl "$DEVICE_URL/screenshot/0?minicap=false"
```

## 获取当前程序版本
```bash
$ curl $DEVICE_URL/version
# expect example: 0.0.2
```

## 获取设备信息
```bash
$ curl $DEVICE_URL/info
{
    "udid": "bf755cab-ff:ff:ff:ff:ff:ff-SM901",
    "serial": "bf755cab",
    "brand": "SMARTISAN",
    "model": "SM901",
    "hwaddr": "ff:ff:ff:ff:ff:ff",
    "agentVersion": "dev"
}
```

## 获取Hierarchy
这个接口目前比较高级，当跟uiautomator通信失败的时候，它会在后台启动uiautomator这个服务，等它恢复正常了，在返回数据。

```bash
$ curl $DEVICE_URL/dump/hierarchy
{
    "jsonrpc":"2.0",
    "id":1559113464,
    "result": "<?xml version='1.0> ... hierarchy ..."
}

# 停止掉uiautomator
$ curl -X DELETE $DEVICE_URL/uiautomator
Success

# 再次调用, 依然OK，只是可能要等个7~8s
$ curl $DEVICE_URL/dump/hierarchy
{
    "jsonrpc": "2.0",
    ...
}
```

## 安装应用
```bash
$ curl -X POST -d url="http://some-host/some.apk" $DEVICE_URL/install
# expect install id
2
# get install progress
$ curl -X GET $DEVICE_URL/install/1
{
    "id": "2",
    "titalSize": 770571,
    "copiedSize": 770571,
    "message": "success installed"
}
```

## Shell命令
```bash
$ curl -X POST -d command="pwd" $DEVICE_URL/shell
{
    "output": "/",
    "error": null
}
```

后台Shell命令(可以在后台持续运行，不会被杀掉)

```bash
$ curl -X POST -d command="pwd" $DEVICE_URL/shell/background
{
    "success": true,
}
```

## Webview相关
```bash
$ curl -X GET $DEVICE_URL/webviews
[
    "webview_devtools_remote_m6x_21074",
    "webview_devtools_remote_m6x_27681",
    "chrome_devtools_remote"
]
```


## App信息获取
```bash
# 获取所有运行的应用
$ curl $DEVICE_URL/proc/list
[
    {
        "cmdline": ["/system/bin/adbd", "--root_seclabel=u:r:su:s0"],
        "name": "adbd",
        "pid": 16177
    },
    {
        "cmdline": ["com.netease.cloudmusic"],
        "name": "com.netease.cloudmusic",
        "pid": 15532
    }
]

# 获取应用的内存信息(数据仅供参考)，单位KB，total代表应用的PSS
$ curl $DEVICE_URL/proc/com.netease.cloudmusic/meminfo
{
    "code": 17236,
    "graphics": 20740,
    "java heap": 22288,
    "native heap": 20576,
    "private other": 10632,
    "stack": 48,
    "system": 110925,
    "total": 202445,
    "total swap pss": 88534
}

# 获取应用以及其所有子进程的内存数据
$ curl $DEVICE_URL/proc/com.netease.cloudmusic/meminfo/all
{
    "com.netease.cloudmusic": {
        "code": 15952,
        "graphics": 19328,
        "java heap": 45488,
        "native heap": 20840,
        "private other": 4056,
        "stack": 956,
        "system": 18652,
        "total": 125272
    },
    "com.netease.cloudmusic:browser": {
        "code": 848,
        "graphics": 12,
        "java heap": 6580,
        "native heap": 5428,
        "private other": 1592,
        "stack": 336,
        "system": 10603,
        "total": 25399
    }
}
```

# 获取CPU信息

如果进程是多线程运行的话，且机器是多核的，返回的CPU Percent可能会大于100%

```bash
curl $DEVICE_URL/proc/<package or pid>/cpuinfo
# success return
{
    "pid": 1122,
    "user": 288138,
    "system": 73457,
    "percent": 50.0,
    "systemPercent": 88.372,
    "coreCount": 4,
}
# failure return
410 Gone, Or 500 Internal error
```


## 下载文件
```bash
$ curl $DEVICE_URL/raw/sdcard/tmp.txt
```

## 上传文件
```bash
# 上传到/sdcard目录下 (url以/结尾)
$ curl -F "file=@somefile.txt" $DEVICE_URL/upload/sdcard/

# 上传到/sdcard/tmp.txt
$ curl -F "file=@somefile.txt" $DEVICE_URL/upload/sdcard/tmp.txt
```

上传目录（url必须以/结尾)

```bash
$ curl -F file=@some.zip -F dir=true $DEVICE_URL/upload/sdcard/
```

## 获取文件和目录信息
```bash
# 文件
$ curl -X GET $DEVICE_URL/finfo/data/local/tmp/tmp.txt
{
	"name": "tmp.txt",
	"path": "/data/local/tmp/tmp.txt",
	"isDirectory": false,
	"size": 15232,
}

# 目录
$ curl -X GET $DEVICE_URL/finfo/data/local/tmp
{
	"name": "tmp",
	"path": "/data/local/tmp",
	"isDirectory": true,
	"size": 8192,
	"files": [
		{
			"name": "tmp.txt", 
			"path": "/data/local/tmp/tmp.txt"
			"isDirectory": false,
		}
	]
}
```

相当于将`some.zip`上传到手机，然后执行`unzip some.zip -d /sdcard`, 最后将`some.zip`删除

## 离线下载
```bash
# 离线下载，返回ID
$ curl -F url=https://.... -F filepath=/sdcard/some.txt -F mode=0644 $DEVICE_URL/download
1
# 通过返回的ID查看下载状态
$ curl $DEVICE_URL/download/1
{
    "message": "downloading",
    "progress": {
        "totalSize": 15000,
        "copiedSize": 10000
    }
}
```

## uiautomator起停
```bash
# 启动
$ curl -X POST $DEVICE_URL/uiautomator
Success

# 停止
$ curl -X DELETE $DEVICE_URL/uiautomator
Success

# 再次停止
$ curl -X DELETE $DEVICE_URL/uiautomator
Already stopped

# 获取uiautomator状态
$ curl $DEVICE/uiautomator
{
    "running": true
}
```

## 启动应用
```bash
# timeout 代表 am start -n 的超时时间
# flags 默认为 -S -W
$ http POST $DEVICE_URL/session/{com.cleanmaster.mguard_cn} timeout==10s flags=="-S"
{
    "mainActivity": "com.keniu.security.main.MainActivity",
    "output": "Stopping: com.cleanmaster.mguard_cn\nStarting: Intent { cmp=com.cleanmaster.mguard_cn/com.keniu.security.main.MainActivity }\n",
    "success": true
}
```

## 获取包信息
```bash
$ http GET $DEVICE_URL/packages/{packageName}/info
{
    "success": true,
    "data": {
        "mainActivity": "com.github.uiautomator.MainActivity",
        "label": "ATX",
        "versionName": "1.1.7",
        "versionCode": 1001007,
        "size":1760809
    }
}
```

其中`size`单位为字节

## 获取包的图标
```
$ curl -XGET $DEVICE_URL/packages/{packageName}/icon
# 返回包的图标文件
# 失败的情况 status code != 200
```

## 获取所有包的信息
该接口速度有点慢，大约需要3s。

原理是通过`pm list packages -3 -f`获取包的信息，然后在用`androidbinary`库对包进行解析

```bash
$ http GET $DEVICE_URL/packages
[
    {
        "packageName": "com.github.uiautomator",
        "mainActivity": "com.github.uiautomator.MainActivity",
        "label": "ATX",
        "versionName": "1.1.7-2-361182f-dirty",
        "versionCode": 1001007,
        "size": 1639366
    },
    {
        "packageName": "com.smartisanos.payment",
        "mainActivity": "",
        "label": "",
        "versionName": "1.1",
        "versionCode": 1,
        "size": 3910826
    },
    ...
]
```

## 调整uiautomator自动停止时间 （默认3分钟）
```bash
$ curl -X POST 10.0.0.1:7912/newCommandTimeout --data 300
{
    "success": true,
    "description":"newCommandTimeout updated to 5m0s"
}
```

## 程序自升级(暂时不能用了)
升级程序从gihub releases里面直接下载，升级完后自动重启

升级到最新版

```bash
$ curl 10.0.0.1:7912/upgrade
```

指定升级的版本

```bash
$ curl "10.0.0.1:7912/upgrade?version=0.0.2"
```

## 修复minicap, minitouch程序

```bash
# Fix minicap 
$ curl -XPUT 10.0.0.1:7912/minicap

# Fix minitouch
$ curl -XPUT 10.0.0.1:7912/minitouch
```

## 视频录制(不推荐用)
开始录制

```bash
$ curl -X POST 10.0.0.1:7912/screenrecord
```

停止录制并获取录制结果

```bash
$ curl -X PUT 10.0.0.1:7912/screenrecord
{
    "videos": [
        "/sdcard/screenrecords/0.mp4",
        "/sdcard/screenrecords/1.mp4"
    ]
}
```

之后再下载到本地

```bash
$ curl -X GET 10.0.0.1:7912/raw/sdcard/screenrecords/0.mp4
```

## Minitouch操作方法
感谢 [openstf/minitouch](https://github.com/openstf/minitouch)

Websocket连接 `$DEVICE_URL/minitouch`, 一行行的按照JSON的格式写入

>注: 坐标原点始终是手机正放时候的左上角，使用者需要自己处理旋转的变化

请先详细阅读minitouch的[Usage](https://github.com/openstf/minitouch#usage)文档，再来看下面的部分

- Touch Down

    坐标(X: 50%, Y: 50%), index代表第几个手指, `pressure`是可选的。

    ```json
    {"operation": "d", "index": 0, "xP": 0.5, "yP": 0.5, "pressure": 50}
    ```

- Touch Commit

    ```json
    {"operation": "c"}
    ```

- Touch Move

    ```json
    {"operation": "m", "index": 0, "xP": 0.5, "yP": 0.5, "pressure": 50}
    ```

- Touch Up

    ```json
    {"operation": "u", "index": 0}
    ```

- 点击x:20%, y:20,滑动到x:40%, y:50%

    ```json
    {"operation": "d", "index": 0, "xP": 0.20, "yP": 0.20, "pressure": 50}
    {"operation": "c"}
    {"operation": "m", "index": 0, "xP": 0.40, "yP": 0.50, "pressure": 50}
    {"operation": "c"}
    {"operation": "u", "index": 0}
    {"operation": "c"}
    ```

# TODO
1. 目前安全性还是个问题，以后再想办法改善
2. 补全接口文档
3. 内置的网页adb shell的安全问题

# Logs
log path `/sdcard/atx-agent.log`

## TODO
- [ ] 使用支持多线程下载的库 https://github.com/cavaliercoder/grab

# LICENSE
[MIT](LICENSE)
