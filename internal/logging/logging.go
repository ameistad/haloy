package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
)

type Logger struct {
	writer io.Writer
	file   *os.File // store the file for later closing
	Level  LogLevel
	mutex  sync.Mutex
	isCLI  bool // isCLI indicates if the logger is used in a CLI context and should use ui library.
}

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	SUCCESS
	WARN
	ERROR
	FATAL
)

func NewLogger(level LogLevel, isCLI bool) (*Logger, error) {
	writer := os.Stdout
	return &Logger{writer: writer, Level: level, isCLI: isCLI}, nil
}

func (l *Logger) Debug(msg string) {
	if l.Level <= DEBUG {
		if l.isCLI {
			ui.Debug("%s", msg)
		} else {
			l.mutex.Lock()
			defer l.mutex.Unlock()
			fmt.Fprintf(l.writer, "[DEBUG] %s\n", msg)
		}
	}
}
func (l *Logger) Info(msg string) {
	if l.Level <= INFO {
		if l.isCLI {
			ui.Info("%s", msg)
		} else {
			l.mutex.Lock()
			defer l.mutex.Unlock()
			fmt.Fprintf(l.writer, "[INFO] %s\n", msg)
		}
	}
}

func (l *Logger) Success(msg string) {
	if l.Level <= SUCCESS {
		if l.isCLI {
			ui.Success("%s", msg)
		} else {
			l.mutex.Lock()
			defer l.mutex.Unlock()
			fmt.Fprintf(l.writer, "[SUCCESS] %s\n", msg)
		}
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
	if l.file != nil {
		l.file.Sync()
		l.file.Close()
	}
	os.Exit(1)
}
func (l *Logger) SetDeploymentIDFileWriter(logsPath string, deploymentID string) error {
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
	// Store file for later closure and log to both os.Stdout and the file.
	l.file = file
	l.writer = io.MultiWriter(os.Stdout, file)
	return nil
}

func (l *Logger) CloseLog() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	fmt.Fprintln(l.writer, "[LOG END]")
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

func CleanOldLogs(maxAgeDays int) error {
	logsPath, err := config.LogsPath()
	if err != nil {
		return fmt.Errorf("failed to get logs path: %w", err)
	}
	files, err := os.ReadDir(logsPath)
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(logsPath, file.Name()))
		}
	}
	return nil
}
