set GOOS=linux
set GOARCH=arm
set GOARM=7
set GOPROXY=https://goproxy.io
set GO111MODULE=on
go get -v github.com/shurcooL/vfsgen
go generate
go build -tags vfs