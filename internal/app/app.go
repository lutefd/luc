package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	tea "charm.land/bubbletea/v2"
	"github.com/lutefd/luc/internal/auth"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/rpc"
	"github.com/lutefd/luc/internal/tui"
	"github.com/lutefd/luc/internal/workspace"
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
	case "pkg":
		info, err := workspace.Detect(cwd)
		if err != nil {
			return err
		}
		return runPkg(info.Root, args[1:])
	case "auth":
		return runAuth(args[1:])
	case "rpc":
		return runRPC(ctx, cwd, args[1:])
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runPkg(workspaceRoot string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: luc pkg <install|remove|list|inspect|pack|validate> ...")
	}

	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("pkg install", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		scopeFlag := fs.String("scope", string(extensions.PackageScopeUser), "")
		yes := fs.Bool("yes", false, "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) != 1 {
			return fmt.Errorf("usage: luc pkg install <source> [--scope user|project] [--yes]")
		}
		scope, err := extensions.ParsePackageScope(*scopeFlag, false)
		if err != nil {
			return err
		}
		result, err := extensions.InstallPackage(workspaceRoot, fs.Args()[0], extensions.InstallOptions{
			Scope:  scope,
			Yes:    *yes,
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
		})
		if err != nil {
			return err
		}
		if result.AlreadyInstalled {
			fmt.Printf("already installed %s@%s in %s scope\n", result.Record.Module, result.Record.Version, result.Record.Scope)
		} else {
			fmt.Printf("installed %s@%s in %s scope\npath: %s\nsource: %s (%s)\n", result.Record.Module, result.Record.Version, result.Record.Scope, result.Record.PackageDir, result.Record.Source, result.Record.SourceType)
		}
		if len(result.Categories) > 0 {
			fmt.Printf("categories: %s\n", strings.Join(result.Categories, ", "))
		}
		if result.ReloadRequired {
			fmt.Println("reload required: run `luc reload` or press ctrl+r in the TUI")
		}
		return nil
	case "remove":
		fs := flag.NewFlagSet("pkg remove", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		scopeFlag := fs.String("scope", string(extensions.PackageScopeUser), "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) != 1 {
			return fmt.Errorf("usage: luc pkg remove <module> [--scope user|project]")
		}
		scope, err := extensions.ParsePackageScope(*scopeFlag, false)
		if err != nil {
			return err
		}
		record, removed, err := extensions.RemoveInstalledPackage(workspaceRoot, fs.Args()[0], scope)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("package %q is not installed in %s scope", fs.Args()[0], scope)
		}
		fmt.Printf("removed %s@%s from %s scope\n", record.Module, record.Version, record.Scope)
		fmt.Println("reload required: run `luc reload` or press ctrl+r in the TUI")
		return nil
	case "list":
		fs := flag.NewFlagSet("pkg list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		scopeFlag := fs.String("scope", string(extensions.PackageScopeAll), "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) != 0 {
			return fmt.Errorf("usage: luc pkg list [--scope user|project|all]")
		}
		scope, err := extensions.ParsePackageScope(*scopeFlag, true)
		if err != nil {
			return err
		}
		packages, err := extensions.ListInstalledPackages(workspaceRoot, scope)
		if err != nil {
			return err
		}
		if len(packages) == 0 {
			fmt.Println("no packages installed")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		fmt.Fprintln(tw, "SCOPE\tMODULE\tVERSION\tSOURCE")
		for _, pkg := range packages {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", pkg.Record.Scope, pkg.Record.Module, pkg.Record.Version, pkg.Record.Source)
		}
		return tw.Flush()
	case "inspect":
		fs := flag.NewFlagSet("pkg inspect", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		scopeFlag := fs.String("scope", string(extensions.PackageScopeAll), "")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if len(fs.Args()) != 1 {
			return fmt.Errorf("usage: luc pkg inspect <module> [--scope user|project|all]")
		}
		scope, err := extensions.ParsePackageScope(*scopeFlag, true)
		if err != nil {
			return err
		}
		packages, err := extensions.InspectInstalledPackages(workspaceRoot, fs.Args()[0], scope)
		if err != nil {
			return err
		}
		if len(packages) == 0 {
			return fmt.Errorf("package %q is not installed", fs.Args()[0])
		}
		for i, pkg := range packages {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("%s@%s (%s scope)\n", pkg.Record.Module, pkg.Record.Version, pkg.Record.Scope)
			fmt.Printf("path: %s\n", pkg.Record.PackageDir)
			fmt.Printf("source: %s (%s)\n", pkg.Record.Source, pkg.Record.SourceType)
			if strings.TrimSpace(pkg.Record.SourceRevision) != "" {
				fmt.Printf("source_revision: %s\n", pkg.Record.SourceRevision)
			}
			if strings.TrimSpace(pkg.Manifest.Name) != "" {
				fmt.Printf("name: %s\n", pkg.Manifest.Name)
			}
			if strings.TrimSpace(pkg.Manifest.Description) != "" {
				fmt.Printf("description: %s\n", pkg.Manifest.Description)
			}
			fmt.Printf("luc_version: %s\n", pkg.Manifest.LucVersion)
			if len(pkg.Categories) > 0 {
				fmt.Printf("categories: %s\n", strings.Join(pkg.Categories, ", "))
			}
			if len(pkg.ExecutableCategories) > 0 {
				fmt.Printf("executable_categories: %s\n", strings.Join(pkg.ExecutableCategories, ", "))
			}
		}
		return nil
	case "pack":
		if len(args) != 2 {
			return fmt.Errorf("usage: luc pkg pack <path>")
		}
		archivePath, validation, err := extensions.PackPackage(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("packed %s@%s\narchive: %s\n", validation.Manifest.Module, validation.Manifest.Version, archivePath)
		return nil
	case "validate":
		if len(args) != 2 {
			return fmt.Errorf("usage: luc pkg validate <path>")
		}
		validation, err := extensions.ValidatePackagePath(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("valid package %s@%s\n", validation.Manifest.Module, validation.Manifest.Version)
		if len(validation.Categories) > 0 {
			fmt.Printf("categories: %s\n", strings.Join(validation.Categories, ", "))
		}
		return nil
	default:
		return fmt.Errorf("unknown pkg command %q", args[0])
	}
}

func runAuth(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: luc auth <set|unset|list> ...")
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: luc auth set <provider-id> <key>")
		}
		if err := auth.Set(args[1], args[2]); err != nil {
			return fmt.Errorf("failed to store credential: %w", err)
		}
		fmt.Printf("credential stored for %q\n", args[1])
		return nil
	case "unset":
		if len(args) != 2 {
			return fmt.Errorf("usage: luc auth unset <provider-id>")
		}
		if err := auth.Delete(args[1]); err != nil {
			if err == auth.ErrNotFound {
				return fmt.Errorf("no credential found for %q", args[1])
			}
			return fmt.Errorf("failed to remove credential: %w", err)
		}
		fmt.Printf("credential removed for %q\n", args[1])
		return nil
	case "list":
		known := []string{"openai", "openai-compatible", "anthropic", "meli"}
		found := auth.List(known)
		if len(found) == 0 {
			fmt.Println("no credentials stored")
			return nil
		}
		for _, id := range found {
			fmt.Println(id)
		}
		return nil
	default:
		return fmt.Errorf("unknown auth command %q (expected set, unset, or list)", args[0])
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
