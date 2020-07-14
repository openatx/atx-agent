package logger

// import
import (

	// "github.com/qiniu/log"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Default *logrus.Logger

func init() {
	// Default = log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)
	Default = logrus.New()
	// logrus.Rep
	Default.SetLevel(logrus.DebugLevel)
}

func SetOutputFile(filename string) error {
	// f, err := os.Create(filename)
	// if err != nil {
	// 	return err
	// }
	Default.SetOutput(&lumberjack.Logger{
		Filename:   filename,
		MaxSize:    100, // megabytes
		MaxBackups: 3,
		MaxAge:     1,    //days
		Compress:   true, // disabled by default
	})
	return nil
	// Default = log.New(out, "", log.LstdFlags|log.Lshortfile)
	// Default.SetOutputLevel(log.Ldebug)
}
