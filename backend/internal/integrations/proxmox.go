// Package integrations implements read-only discovery connectors. Syncs never
// delete manually maintained assets, never overwrite operator fields, and mark
// imported data with source attribution.
package integrations

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/petruknisme/lanvory/internal/store"
)

// ProxmoxClient talks to a Proxmox VE API with a read-only API token.
type ProxmoxClient struct {
	baseURL    string
	tokenID    string
	tokenSecret string
	http       *http.Client
}

func NewProxmoxClient(endpoint, tokenID, tokenSecret string, verifyTLS bool) *ProxmoxClient {
	return &ProxmoxClient{
		baseURL:     endpoint,
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS}, //nolint:gosec // user-controlled self-signed clusters
			},
		},
	}
}

func (c *ProxmoxClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proxmox %s: HTTP %d: %s", path, resp.StatusCode, truncateStr(string(body), 200))
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("proxmox %s: %w", path, err)
	}
	if len(envelope.Data) == 0 {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// SyncProxmox imports nodes and guests (VMs + LXC) as source-managed assets.
// Observed power state is recorded under metadata.observed — declared status is untouched.
func SyncProxmox(ctx context.Context, pool *pgxpool.Pool, integ store.Integration, secretValue string) (map[string]any, error) {
	var cfg struct {
		Endpoint  string `json:"endpoint"`
		TokenID   string `json:"token_id"`
		VerifyTLS bool   `json:"verify_tls"`
	}
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.Endpoint == "" || cfg.TokenID == "" || secretValue == "" {
		return nil, fmt.Errorf("endpoint, token_id and the API token secret are required")
	}

	client := NewProxmoxClient(cfg.Endpoint, cfg.TokenID, secretValue, cfg.VerifyTLS)
	integrations := store.NewIntegrations(pool)

	created, updated := 0, 0

	type proxmoxNode struct {
		Node   string `json:"node"`
		Status string `json:"status"`
	}
	var nodes []proxmoxNode
	if err := client.get(ctx, "/api2/json/nodes", &nodes); err != nil {
		return nil, err
	}

	declaredStatus := func(observed string) string {
		// Observed state must not overwrite operator-declared status: recorded
		// in metadata.observed only. New assets start as "unknown".
		return "unknown"
	}

	for _, node := range nodes {
		nodeAssetID, wasCreated, err := integrations.UpsertDiscoveredAsset(ctx, store.DiscoveredAsset{
			Name: node.Node, Hostname: node.Node, TypeSlug: "server",
			Status: declaredStatus(node.Status), Environment: "production",
			OSName: "Proxmox VE", Source: "proxmox",
			ExternalKey: "node", ExternalValue: node.Node,
			Metadata: map[string]any{
				"proxmox": map[string]any{"node": node.Node, "value": node.Node},
				"observed": map[string]any{"status": node.Status, "checked_at": time.Now().UTC().Format(time.RFC3339)},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", node.Node, err)
		}
		if wasCreated {
			created++
		} else {
			updated++
		}

		// Guests: QEMU VMs.
		var vms []struct {
			VMID    int    `json:"vmid"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			MaxMem  int64  `json:"maxmem"`
			MaxCPU  int    `json:"maxcpu"`
			Template int   `json:"template"`
		}
		if err := client.get(ctx, "/api2/json/nodes/"+node.Node+"/qemu", &vms); err != nil {
			return nil, fmt.Errorf("qemu %s: %w", node.Node, err)
		}
		for _, vm := range vms {
			if vm.Name == "" || vm.Template == 1 {
				continue
			}
			if wasCreatedNow, err := upsertGuest(ctx, integrations, store.DiscoveredAsset{
				Name: vm.Name, Hostname: vm.Name, TypeSlug: "vm",
				Environment: "production", Source: "proxmox",
				ExternalKey: "vmid", ExternalValue: strconv.Itoa(vm.VMID),
				CPUCores: intPtr(vm.MaxCPU), RamGB: float64Ptr(bytesToGB(vm.MaxMem)),
				Metadata: map[string]any{
					"proxmox": map[string]any{"vmid": vm.VMID, "node": node.Node, "value": strconv.Itoa(vm.VMID)},
					"observed": map[string]any{"status": vm.Status, "checked_at": time.Now().UTC().Format(time.RFC3339)},
				},
			}, nodeAssetID); err != nil {
				return nil, fmt.Errorf("vm %d: %w", vm.VMID, err)
			} else if wasCreatedNow {
				created++
			} else {
				updated++
			}
		}

		// Guests: LXC containers.
		var lxcs []struct {
			VMID   int    `json:"vmid"`
			Name   string `json:"name"`
			Status string `json:"status"`
			MaxMem int64  `json:"maxmem"`
			MaxCPU int    `json:"maxcpu"`
		}
		if err := client.get(ctx, "/api2/json/nodes/"+node.Node+"/lxc", &lxcs); err != nil {
			return nil, fmt.Errorf("lxc %s: %w", node.Node, err)
		}
		for _, ct := range lxcs {
			if ct.Name == "" {
				continue
			}
			wasCreatedNow, err := upsertGuest(ctx, integrations, store.DiscoveredAsset{
				Name: ct.Name, Hostname: ct.Name, TypeSlug: "lxc",
				Environment: "production", Source: "proxmox",
				ExternalKey: "vmid", ExternalValue: strconv.Itoa(ct.VMID),
				CPUCores: intPtr(ct.MaxCPU), RamGB: float64Ptr(bytesToGB(ct.MaxMem)),
				Metadata: map[string]any{
					"proxmox": map[string]any{"vmid": ct.VMID, "node": node.Node, "value": strconv.Itoa(ct.VMID)},
					"observed": map[string]any{"status": ct.Status, "checked_at": time.Now().UTC().Format(time.RFC3339)},
				},
			}, nodeAssetID)
			if err != nil {
				return nil, fmt.Errorf("lxc %d: %w", ct.VMID, err)
			}
			if wasCreatedNow {
				created++
			} else {
				updated++
			}
		}
	}

	return map[string]any{
		"nodes": len(nodes), "created": created, "updated": updated,
	}, nil
}

// upsertGuest inserts or refreshes a guest and parents it to its node.
func upsertGuest(ctx context.Context, integrations *store.Integrations, guest store.DiscoveredAsset, nodeAssetID string) (bool, error) {
	id, created, err := integrations.UpsertDiscoveredAsset(ctx, guest)
	if err != nil {
		return false, err
	}
	// Ensure parent link: set parent_id only when unset (never reparent manually-placed guests).
	if err := integrations.EnsureParent(ctx, id, nodeAssetID); err != nil {
		return false, err
	}
	return created, nil
}

func intPtr(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func float64Ptr(f float64) *float64 {
	if f == 0 {
		return nil
	}
	return &f
}

func bytesToGB(b int64) float64 {
	return float64(b) / 1e9
}
