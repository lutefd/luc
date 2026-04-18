package app

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/tui"
)

func Run(ctx context.Context, args []string) error {
	command := "tui"
	if len(args) > 0 {
		command = args[0]
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	controller, err := kernel.New(ctx, cwd)
	if err != nil {
		return err
	}

	switch command {
	case "tui":
		p := tea.NewProgram(tui.New(controller), tea.WithAltScreen())
		_, err := p.Run()
		return err
	case "doctor":
		fmt.Printf("workspace: %s\nproject_id: %s\nprovider: %s\nmodel: %s\nsession: %s\n", controller.Workspace().Root, controller.Workspace().ProjectID, controller.Config().Provider.Kind, controller.Config().Provider.Model, controller.Session().SessionID)
		return nil
	case "reload":
		return controller.Reload(ctx)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}
