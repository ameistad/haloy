package ui

import (
	"github.com/fatih/color"
)

var (
	Success = color.New(color.FgGreen).Add(color.Bold).PrintfFunc()
	Info    = color.New(color.FgCyan).PrintfFunc()
	Debug   = color.New(color.FgWhite).PrintfFunc()
	Command = color.New(color.FgYellow).PrintfFunc()
	Warn    = color.New(color.FgYellow).Add(color.Bold).PrintfFunc()
	Error   = color.New(color.FgRed).Add(color.Bold).PrintfFunc()
)
