arm:
	GOOS=linux GOARCH=arm go build -tags vfs

clean:
	@rm -f atx-agent

all: arm
