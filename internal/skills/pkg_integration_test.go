package skills

import (
	"os/exec"
	"testing"
)

func TestIntegrationDebianContainerPlaceholder(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	t.Skip("integration test scaffold: run apt scenarios in Debian container in CI")
}
