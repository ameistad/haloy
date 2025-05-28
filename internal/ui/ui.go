package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/pterm/pterm"
)

// Colors
var (
	White     = lipgloss.Color("#FAFAFA")
	Gray      = lipgloss.Color("245")
	LightGray = lipgloss.Color("241")
)

// Styles
var titleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(White)

func Success(format string, a ...any) {
	pterm.Success.Println(fmt.Sprintf(format, a...))
}
func Info(format string, a ...any) {
	pterm.Info.Println(fmt.Sprintf(format, a...))
}

func Debug(format string, a ...any) {
	pterm.Debug.Println(fmt.Sprintf(format, a...))
}

func Warn(format string, a ...any) {
	pterm.Warning.Println(fmt.Sprintf(format, a...))
}

func Error(format string, a ...any) {
	pterm.Error.Println(fmt.Sprintf(format, a...))
}

func BoldText(format string, a ...any) string {
	return pterm.Bold.Sprint(fmt.Sprintf(format, a...))
}

var lineStyle = lipgloss.NewStyle().
	Foreground(White).
	TabWidth(5)

func Section(title string, textLines []string) {

	fmt.Println(titleStyle.BorderStyle(lipgloss.NormalBorder()).BorderForeground(White).BorderBottom(true).Render(title))
	for _, line := range textLines {
		fmt.Println(lineStyle.Render(line))
	}
}

func Table(headers []string, rows [][]string) {

	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(Gray)).
		StyleFunc(func(row, col int) lipgloss.Style {
			switch {
			case row == table.HeaderRow:
				return titleStyle.Align(lipgloss.Center)
			case row%2 == 0:
				return cellStyle.Foreground(LightGray)
			default:
				return cellStyle.Foreground(Gray)
			}
		}).
		Headers(headers...).
		Rows(rows...)
	fmt.Println(t)
}
