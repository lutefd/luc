package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/rpc"
	"github.com/lutefd/luc/internal/tui"
)

func Run(ctx context.Context, args []string) error {
	if hasModeFlag(args) {
		return runModeAlias(ctx, args)
	}

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
	case "rpc":
		return runRPC(ctx, cwd, args[1:])
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func hasModeFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--mode" || strings.HasPrefix(arg, "--mode=") {
			return true
		}
	}
	return false
}

func runModeAlias(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("luc", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	mode := fs.String("mode", "", "")
	sessionID := fs.String("session", "", "")
	continueLatest := fs.Bool("continue", false, "")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*mode) != "rpc" {
		return fmt.Errorf("unsupported mode %q", *mode)
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments for rpc mode: %s", strings.Join(fs.Args(), " "))
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return runRPCWithSelection(ctx, cwd, *sessionID, *continueLatest)
}

func runRPC(ctx context.Context, cwd string, args []string) error {
	fs := flag.NewFlagSet("rpc", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	sessionID := fs.String("session", "", "")
	continueLatest := fs.Bool("continue", false, "")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments for rpc mode: %s", strings.Join(fs.Args(), " "))
	}
	return runRPCWithSelection(ctx, cwd, *sessionID, *continueLatest)
}

func runRPCWithSelection(ctx context.Context, cwd, sessionID string, continueLatest bool) error {
	if strings.TrimSpace(sessionID) != "" && continueLatest {
		return fmt.Errorf("--session and --continue cannot be used together")
	}

	var (
		controller *kernel.Controller
		err        error
	)
	switch {
	case strings.TrimSpace(sessionID) != "":
		controller, err = kernel.Open(ctx, cwd, strings.TrimSpace(sessionID))
	case continueLatest:
		controller, err = kernel.ResumeLatest(ctx, cwd)
	default:
		controller, err = kernel.New(ctx, cwd)
	}
	if err != nil {
		return err
	}

	return rpc.Run(ctx, rpc.Options{
		Controller: controller,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
	})
}
