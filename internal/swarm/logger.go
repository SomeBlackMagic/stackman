package swarm

import (
	"fmt"
	"log"
)

// Logger is a minimal logging abstraction for dependency injection.
type Logger interface {
	Printf(format string, v ...any)
}

type stdLogger struct{}

func (stdLogger) Printf(format string, v ...any) {
	log.Printf(format, v...)
}

func withDefaultLogger(logger Logger) Logger {
	if logger != nil {
		return logger
	}
	return stdLogger{}
}

func (d *StackDeployer) logf(format string, args ...any) {
	d.logger.Printf(format, args...)
}

func (d *StackDeployer) logln(args ...any) {
	d.logger.Printf("%s", fmt.Sprint(args...))
}
