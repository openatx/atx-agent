package collector

import (
	"github.com/go-kit/kit/log"
)

func (n NodeCollector) SetLogger(l log.Logger) {
	n.logger = l
}

func (n NodeCollector) GetLogger() log.Logger {
	return n.logger
}
