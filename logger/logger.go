package logger

// import
import (
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Default *logrus.Logger

func init() {
	Default = logrus.New()
	Default.SetLevel(logrus.DebugLevel)
}

func SetOutputFile(filename string) error {
	Default.SetOutput(&lumberjack.Logger{
		Filename:   filename,
		MaxSize:    100, // megabytes
		MaxBackups: 3,
		MaxAge:     1,    //days
		Compress:   true, // disabled by default
	})
	return nil
}
