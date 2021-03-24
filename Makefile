all: x86 arm arm64
x86: pre
	GOOS=linux GOARCH=386 go build -o bin/atx-agent.x86 -tags vfs
arm: pre
	GOOS=linux GOARCH=arm go build -o bin/atx-agent.arm -tags vfs 
arm64:pre
	GOOS=linux GOARCH=arm64 go build -o bin/atx-agent.arm64 -tags vfs 
pre:
	go mod tidy
	go get github.com/shurcooL/vfsgen
	go generate