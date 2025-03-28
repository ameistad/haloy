package ui

import (
	"github.com/fatih/color"
)

// Text styling functions
var (
	Success = color.New(color.FgGreen).Add(color.Bold).PrintfFunc()
	Info    = color.New(color.FgCyan).PrintfFunc()
	Command = color.New(color.FgYellow).PrintfFunc()
	Warning = color.New(color.FgYellow).Add(color.Bold).PrintfFunc()
	Error   = color.New(color.FgRed).Add(color.Bold).PrintfFunc()
)
