# atx-agent
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

# 常用接口
假设手机的ip是10.0.0.1

## 获取手机截图
```bash
$ curl 10.0.0.1:7912/screenshot
# jpeg format image
```

## 获取当前程序版本
```bash
$ curl 10.0.0.1:7912/version
# expect example: 0.0.2
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

# Logs
log path `/sdcard/atx-agent.log`

# Build from source
```bash
GOOS=linux GOARCH=arm go build
```

# LICENSE
[MIT](LICENSE)