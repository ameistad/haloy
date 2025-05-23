package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
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

func BoldText(format string, a ...any) string {
	return pterm.Bold.Sprint(fmt.Sprintf(format, a...))
}

var titleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#FAFAFA"))

var lineStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#FAFAFA"))

func Section(title string, textLines []string) {

	fmt.Println(titleStyle.Render(title))

	for _, line := range textLines {
		fmt.Println(lineStyle.Render(line))
	}
}
