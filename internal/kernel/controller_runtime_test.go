package kernel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
)

func TestControllerInstallsDefaultUIBrokerAndHostCapabilities(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return &fakeProvider{}, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := controller.UIBroker().(*luruntime.DefaultBroker); !ok {
		t.Fatalf("expected default broker, got %T", controller.UIBroker())
	}
	caps := controller.HostCapabilities()
	if len(caps) == 0 {
		t.Fatal("expected host capabilities")
	}
	foundConfirm := false
	for _, capability := range caps {
		if capability == luruntime.HostCapabilityUIConfirm {
			foundConfirm = true
			break
		}
	}
	if !foundConfirm {
		t.Fatalf("expected confirm capability, got %#v", caps)
	}
}
