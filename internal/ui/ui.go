package ui

import (
	"strings"

	"github.com/fatih/color"
	"github.com/pterm/pterm"
)

var (
	Success = pterm.Success.Println
	Info    = pterm.Info.Println
	Debug   = color.New(color.FgWhite).PrintfFunc()
	Command = color.New(color.FgYellow).PrintfFunc()
	Warn    = color.New(color.FgYellow).Add(color.Bold).PrintfFunc()
	Error   = color.New(color.FgRed).Add(color.Bold).PrintfFunc()
)

func Section(title string, textLines []string) {
	lines := strings.Join(textLines, "\n")
	pterm.DefaultSection.Println(title)
	pterm.Info.Println(lines)
}
