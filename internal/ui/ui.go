package ui

import (
	"strings"

	"github.com/pterm/pterm"
)

var (
	Success = pterm.Success.Println
	Info    = pterm.Info.Println
	Debug   = pterm.Debug.Println
	Warn    = pterm.Warning.Println
	Error   = pterm.Error.Println
)

func Section(title string, textLines []string) {
	lines := strings.Join(textLines, "\n")
	pterm.DefaultSection.Println(title)
	pterm.Info.Println(lines)
}
