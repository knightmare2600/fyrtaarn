package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/knightmare2600/fyrtaarn/internal/tui"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func main() {
	// Optional startup banner (useful for debugging / CI builds)
	fmt.Printf("Fyrtaarn %s (%s) built %s\n", Version, Commit, BuildDate)

	app := tui.NewApp()
	app.Version = Version
	app.Commit = Commit
	app.BuildDate = BuildDate

	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
