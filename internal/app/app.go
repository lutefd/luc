package app

import (
	"context"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
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

	switch command {
	case "tui":
		controller, err := kernel.New(ctx, cwd)
		if err != nil {
			return err
		}
		defer controller.Close()
		p := tea.NewProgram(tui.New(controller))
		_, err = p.Run()
		if controller.SessionSaved() {
			fmt.Printf("to reopen this session, run: luc open %s\n", controller.Session().SessionID)
		}
		return err
	case "open":
		if len(args) < 2 {
			return fmt.Errorf("usage: luc open <session-id>")
		}
		controller, err := kernel.Open(ctx, cwd, args[1])
		if err != nil {
			return err
		}
		defer controller.Close()
		p := tea.NewProgram(tui.New(controller))
		_, err = p.Run()
		if controller.SessionSaved() {
			fmt.Printf("to reopen this session, run: luc open %s\n", controller.Session().SessionID)
		}
		return err
	case "doctor":
		controller, err := kernel.ResumeLatest(ctx, cwd)
		if err != nil {
			return err
		}
		defer controller.Close()
		fmt.Printf("workspace: %s\nproject_id: %s\nprovider: %s\nmodel: %s\nsession: %s\n", controller.Workspace().Root, controller.Workspace().ProjectID, controller.Config().Provider.Kind, controller.Config().Provider.Model, controller.Session().SessionID)
		return nil
	case "reload":
		controller, err := kernel.ResumeLatest(ctx, cwd)
		if err != nil {
			return err
		}
		defer controller.Close()
		return controller.Reload(ctx)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}
