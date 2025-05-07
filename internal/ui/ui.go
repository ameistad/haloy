package ui

import (
	"fmt"
	"strings"

	"github.com/pterm/pterm"
)

var (
	// Success = pterm.Success.Println // Removed direct assignment
	// Info    = pterm.Info.Println    // Removed direct assignment
	Debug = pterm.Debug.Println
	// Warn    = pterm.Warning.Println // Removed direct assignment
	// Error   = pterm.Error.Println   // Removed direct assignment
)

// Success prints a success message with formatting.
func Success(format string, a ...any) {
	pterm.Success.Println(fmt.Sprintf(format, a...))
}

// Info prints an info message with formatting.
func Info(format string, a ...any) {
	pterm.Info.Println(fmt.Sprintf(format, a...))
}

// Warn prints a warning message with formatting.
func Warn(format string, a ...any) {
	pterm.Warning.Println(fmt.Sprintf(format, a...))
}

// Error prints an error message with formatting.
func Error(format string, a ...any) {
	pterm.Error.Println(fmt.Sprintf(format, a...))
}
func Section(title string, textLines []string) {
	lines := strings.Join(textLines, "\n")
	pterm.DefaultSection.Println(title)
	pterm.Info.Println(lines)
}
