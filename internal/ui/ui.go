package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Colors
var (
	White     = lipgloss.Color("#fafafa")
	Gray      = lipgloss.Color("245")
	Green     = lipgloss.Color("#22c55e")
	LightGray = lipgloss.Color("241")
	Red       = lipgloss.Color("#f87171")
	Yellow    = lipgloss.Color("#fef08a")
)

// Styles
var titleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(White)

var baseStyle = lipgloss.NewStyle()

func Success(format string, a ...any) {
	printStyledLines(baseStyle.Foreground(Green).Bold(true), format, a...)
}
func Info(format string, a ...any) {
	printStyledLines(baseStyle.Foreground(White), format, a...)
}

func Debug(format string, a ...any) {
	printStyledLines(baseStyle.Foreground(LightGray), format, a...)
}

func Warn(format string, a ...any) {
	printStyledLines(baseStyle.Foreground(Yellow), format, a...)
}

func Error(format string, a ...any) {
	printStyledLines(baseStyle.Foreground(Red), format, a...)
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

func printStyledLines(style lipgloss.Style, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	lines := strings.Split(msg, "\n")
	for _, line := range lines {
		if line != "" {
			fmt.Println(style.Render(line))
		}
	}
}
