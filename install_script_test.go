package main

import (
	"os"
	"strings"
	"testing"
)

func TestInstallScriptDocumentsAndImplementsUbuntuDeployment(t *testing.T) {
	scriptBytes, err := os.ReadFile("scripts/install.sh")
	if err != nil {
		t.Fatalf("read install script: %v", err)
	}
	scriptInfo, err := os.Stat("scripts/install.sh")
	if err != nil {
		t.Fatalf("stat install script: %v", err)
	}
	if scriptInfo.Mode()&0o111 == 0 {
		t.Fatal("scripts/install.sh is not executable")
	}
	script := string(scriptBytes)

	scriptChecks := []string{
		"set -euo pipefail",
		"ID\" != \"ubuntu",
		"KTDAT_REPO",
		"KTDAT_BRANCH",
		"KTDAT_PATH",
		"KTDAT_TOKEN",
		"load_existing_env",
		"CURRENT_KTDAT_TOKEN",
		"read -r -s",
		"/dev/tty",
		"/etc/kt-proxy/kt-proxy.env",
		"/etc/systemd/system/kt-proxy.service",
		"/usr/local/bin/kt-proxy",
		"github.com/${REPO}/archive/refs/heads/${BRANCH}.tar.gz",
		"systemctl daemon-reload",
		"systemctl enable kt-proxy",
		"systemctl restart kt-proxy",
	}
	for _, want := range scriptChecks {
		if !strings.Contains(script, want) {
			t.Fatalf("scripts/install.sh missing %q", want)
		}
	}

	readmeBytes, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	readme := string(readmeBytes)
	readmeChecks := []string{
		"curl -fsSL https://raw.githubusercontent.com/Van426326/sing-box-web/main/scripts/install.sh | sudo bash",
		"sudo -E bash",
		"/etc/kt-proxy/kt-proxy.env",
		"KTDAT_REPO",
		"KTDAT_BRANCH",
		"KTDAT_PATH",
		"KTDAT_TOKEN",
		"systemctl status kt-proxy",
		"journalctl -u kt-proxy",
	}
	for _, want := range readmeChecks {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing %q", want)
		}
	}
}
