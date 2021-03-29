all: x86 arm arm64 self
self: pre
	go build -o bin/atx-agent -tags vfs
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

bin/atx-agent: self
	bin/atx-agent version 2>/dev/null >latest
upload: bin/atx-agent arm64 arm x86
	bcecmd bos cp bin/atx-agent.x86 bos:/safe-sig/opinit/atx-agent/atx-agent_$(shell cat latest)_linux_386
	bcecmd bos cp bin/atx-agent.arm64 bos:/safe-sig/opinit/atx-agent/atx-agent_$(shell cat latest)_linux_armv7
	bcecmd bos cp bin/atx-agent.arm64 bos:/safe-sig/opinit/atx-agent/atx-agent_$(shell cat latest)_linux_arm64
	bcecmd bos cp bin/atx-agent.arm bos:/safe-sig/opinit/atx-agent/atx-agent_$(shell cat latest)_linux_arm
	bcecmd bos cp latest bos:/safe-sig/opinit/atx-agent/latest
	rm -rf latest