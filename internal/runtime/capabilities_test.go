package runtime

import "testing"

func TestNormalizeCapabilitiesDedupesAndTrims(t *testing.T) {
	got := NormalizeCapabilities([]string{" structured_io ", "", "client_actions", "structured_io"})
	if len(got) != 2 || got[0] != CapabilityStructuredIO || got[1] != CapabilityClientAction {
		t.Fatalf("unexpected normalized capabilities %#v", got)
	}
}

func TestCheckHostRequirementsReportsMissingCapabilities(t *testing.T) {
	result := CheckHostRequirements(
		[]string{HostCapabilityUIConfirm, HostCapabilityUIViewOpen},
		[]string{HostCapabilityUIConfirm},
	)
	if result.Supported() {
		t.Fatalf("expected unsupported result %#v", result)
	}
	if len(result.Missing) != 1 || result.Missing[0] != HostCapabilityUIViewOpen {
		t.Fatalf("unexpected missing capabilities %#v", result.Missing)
	}
}
