package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.Logger

func init() {
	// Initialize a no-op logger so Log is never nil.
	// This prevents nil pointer panics if code runs before InitLogger (e.g. in tests).
	Log = zap.NewNop()
}

func InitLogger(env string) {
	InitLoggerWithOutput(env, "stderr")
}

func InitLoggerWithOutput(env string, outputPaths ...string) {
	var config zap.Config
	if env == "production" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	if len(outputPaths) == 0 {
		config.OutputPaths = []string{"stderr"}
	} else {
		config.OutputPaths = outputPaths
	}
	config.ErrorOutputPaths = []string{"stderr"}

	var err error
	Log, err = config.Build()
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
}

func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
