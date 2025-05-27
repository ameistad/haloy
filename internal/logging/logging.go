package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type Logger struct {
	writer io.Writer
	Level  LogLevel
	mutex  sync.Mutex
}

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

func NewLogger(level LogLevel) (*Logger, error) {
	writer := os.Stdout
	return &Logger{writer: writer, Level: level}, nil
}

func (l *Logger) Debug(msg string) {
	if l.Level <= DEBUG {
		l.mutex.Lock()
		defer l.mutex.Unlock()
		fmt.Fprintf(l.writer, "[DEBUG] %s\n", msg)
	}
}
func (l *Logger) Info(msg string) {
	if l.Level <= INFO {
		l.mutex.Lock()
		defer l.mutex.Unlock()
		fmt.Fprintf(l.writer, "%s\n", msg)
	}
}
func (l *Logger) Warn(msg string, err ...error) {
	if l.Level <= WARN {
		l.mutex.Lock()
		defer l.mutex.Unlock()
		if len(err) > 0 && err[0] != nil {
			fmt.Fprintf(l.writer, "[WARN] %s: %v\n", msg, err[0])
		} else {
			fmt.Fprintf(l.writer, "[WARN] %s\n", msg)
		}
	}
}
func (l *Logger) Error(msg string, err ...error) {
	if l.Level <= ERROR {
		l.mutex.Lock()
		defer l.mutex.Unlock()
		if len(err) > 0 && err[0] != nil {
			fmt.Fprintf(l.writer, "[ERROR] %s: %v\n", msg, err[0])
		} else {
			fmt.Fprintf(l.writer, "[ERROR] %s\n", msg)
		}
	}
}
func (l *Logger) Fatal(msg string, err ...error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if len(err) > 0 && err[0] != nil {
		fmt.Fprintf(l.writer, "[FATAL] %s: %v\n", msg, err[0])
	} else {
		fmt.Fprintf(l.writer, "[FATAL] %s\n", msg)
	}
	if f, ok := l.writer.(*os.File); ok {
		f.Sync()
		f.Close()
	}
	os.Exit(1)
}
func (l *Logger) SetDeploymentIDFileWriter(logsPath, deploymentID string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if deploymentID == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}
	logFilePath := filepath.Join(logsPath, deploymentID+".log")
	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	l.writer = file
	return nil
}
