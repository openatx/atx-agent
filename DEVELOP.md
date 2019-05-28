# Develop doc
需要Go版本 >= 1.11, 这个版本之后可以不用设置GOPATH变量了。

## 安装Go环境
Mac上安装Go

```bash
brew install go
```

## 编译方法
```bash
# 下载代码
git clone https://github.com/openatx/atx-agent
cd atx-agent

# 通过下面的命令就可以设置代理，方便国内用户。国外用户忽略
export GOPROXY=https://goproxy.io

# 使用go.mod管理依赖库
export GO111MODULE=on

# 将assets目录下的文件打包成go代码
go generate

# build for android binary
GOOS=linux GOARCH=arm go build -tags vfs
```
