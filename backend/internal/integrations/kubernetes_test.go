package integrations

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKubeconfigParsing covers the kubeconfig subset: context selection,
// server extraction, token and quantity helpers.
func TestKubeconfigParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: lab
clusters:
- name: lab-cluster
  cluster:
    server: https://10.0.0.5:6443
    insecure-skip-tls-verify: true
- name: other
  cluster:
    server: https://203.0.113.9:6443
contexts:
- name: lab
  context:
    cluster: lab-cluster
    user: admin
- name: other
  context:
    cluster: other
    user: admin
users:
- name: admin
  user:
    token: super-secret-token
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	client, err := clientFromKubeconfig(kubeConfig{Kubeconfig: path})
	if err != nil {
		t.Fatalf("clientFromKubeconfig: %v", err)
	}
	if client.base != "https://10.0.0.5:6443" {
		t.Fatalf("base = %q", client.base)
	}
	if client.token != "super-secret-token" {
		t.Fatalf("token = %q", client.token)
	}

	// Explicit context wins over current-context.
	client, err = clientFromKubeconfig(kubeConfig{Kubeconfig: path, Context: "other"})
	if err != nil {
		t.Fatalf("context override: %v", err)
	}
	if client.base != "https://203.0.113.9:6443" {
		t.Fatalf("context override base = %q", client.base)
	}

	// Unknown context is a clear error.
	if _, err := clientFromKubeconfig(kubeConfig{Kubeconfig: path, Context: "nope"}); err == nil {
		t.Fatalf("expected error for unknown context")
	}
}

func TestQuantityParsing(t *testing.T) {
	if c := parseCPUCores("4"); c == nil || *c != 4 {
		t.Fatalf("cpu 4 → %v", c)
	}
	if c := parseCPUCores("4500m"); c == nil || *c != 5 {
		t.Fatalf("cpu 4500m → %v", c)
	}
	if c := parseCPUCores(""); c != nil {
		t.Fatalf("cpu empty → %v", c)
	}
	if g := parseMemoryGi("16Gi"); g == nil || *g != 16 {
		t.Fatalf("mem 16Gi → %v", g)
	}
	if g := parseMemoryGi("2048Mi"); g == nil || *g != 2 {
		t.Fatalf("mem 2048Mi → %v", g)
	}
	if s := kubernetesSlug("kube-system_coredns"); s != "kube-system-coredns" {
		t.Fatalf("slug = %q", s)
	}
}
