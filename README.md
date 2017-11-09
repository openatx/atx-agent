# atx-agent
[![Build Status](https://travis-ci.org/openatx/atx-agent.svg?branch=master)](https://travis-ci.org/openatx/atx-agent)

HTTP Server runs on android device

运行再Android手机上的http服务器，旨在希望通过Wifi控制手机，完成手机的自动化功能。

# Usage
从<https://github.com/openatx/atx-agent/releases>下载以`linux_armv7.tar.gz`结尾的二进制包。绝大部分手机都是linux-arm架构的。

解压出`atx-agent`文件，然后打开控制台
```bash
$ adb push atx-agent /data/local/tmp
$ adb shell chmod 755 /data/local/tmp/atx-agent
# launch atx-agent in daemon mode
$ adb shell /data/local/tmp/atx-agent -d
```

默认监听的端口是7912。

# 重要更新历史
- 0.0.8

    * 支持连接Server获取最新版本，并自动升级

- 0.0.7

    * 响应服务端的websocket PING请求
    * 如果安装失败，尝试先卸载，然后继续安装

- 0.0.6

    * 支持文件上传

- 0.0.5

    * 移除每次启动时自动安装minicap, com.github.uiautomator应用

- 0.0.4

    * 增加网页版的控制台
    * 支持daemon模式运行

- 0.0.3

    * 增加安装应用支持

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

## 程序自升级
升级程序从gihub releases里面直接下载，升级完后自动重启

升级到最新版

```bash
$ curl 10.0.0.1:7912/upgrade
```

指定升级的版本

```bash
$ curl "10.0.0.1:7912/upgrade?version=0.0.2"
```

# TODO
1. 目前安全性还是个问题，以后再想办法改善
2. 补全接口文档
3. 内置的网页adb shell的安全问题

# Logs
log path `/sdcard/atx-agent.log`

# Build from source
```bash
GOOS=linux GOARCH=arm go build
```

with html resource buildin

```bash
go get github.com/shurcooL/vfsgen
go generate
go build -tags vfs
```

# LICENSE
[MIT](LICENSE)