package lib

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func Logger(level string) (*zap.SugaredLogger, error) {
	zcfg := zap.NewProductionConfig()
	//zcfg := zap.NewDevelopmentConfig()
	loglevel := zapcore.DebugLevel
	_ = loglevel.Set(level)
	zcfg.Level.SetLevel(loglevel)
	zcfg.Sampling = nil
	l, err := zcfg.Build()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize zap logger: %s", err)
	}
	return l.Sugar(), nil
}
