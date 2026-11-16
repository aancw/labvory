package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/petruknisme/lanvory/internal/store"
)

// SyncDocker discovers containers on one Docker host via the Engine API.
// `hostAssetID` anchors every discovered container to that host asset.
func SyncDocker(ctx context.Context, pool *pgxpool.Pool, integ store.Integration, hostAssetID string) (map[string]any, error) {
	var cfg struct {
		Host string `json:"host"` // unix:///path or tcp://host:port
	}
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("docker host is required (unix:///var/run/docker.sock or tcp://host:2375)")
	}

	client, err := dockerHTTPClient(cfg.Host)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/v1.43/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker api: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker api: HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var containers []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
		Image string   `json:"Image"`
		State string   `json:"State"`
	}
	if err := json.Unmarshal(body, &containers); err != nil {
		return nil, fmt.Errorf("docker api: %w", err)
	}

	integrations := store.NewIntegrations(pool)
	created, updated := 0, 0
	now := time.Now().UTC().Format(time.RFC3339)

	for _, c := range containers {
		name := strings.TrimPrefix(firstOrEmpty(c.Names), "/")
		if name == "" {
			continue
		}
		id, wasCreated, err := integrations.UpsertDiscoveredAsset(ctx, store.DiscoveredAsset{
			Name: name, TypeSlug: "docker",
			Environment: "production", Source: "docker",
			ExternalKey: "container_id", ExternalValue: c.ID[:12],
			OSName: c.Image,
			Metadata: map[string]any{
				"docker":   map[string]any{"container_id": c.ID[:12], "image": c.Image, "value": c.ID[:12]},
				"observed": map[string]any{"status": c.State, "checked_at": now},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("container %s: %w", name, err)
		}
		if wasCreated {
			created++
		} else {
			updated++
		}
		if err := integrations.EnsureParent(ctx, id, hostAssetID); err != nil {
			return nil, err
		}
	}

	return map[string]any{"containers": len(containers), "created": created, "updated": updated}, nil
}

func dockerHTTPClient(host string) (*http.Client, error) {
	tr := &http.Transport{}
	if strings.HasPrefix(host, "unix://") {
		socket := strings.TrimPrefix(host, "unix://")
		tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
	} else if strings.HasPrefix(host, "tcp://") {
		tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, strings.TrimPrefix(host, "tcp://"))
		}
	} else {
		return nil, fmt.Errorf("docker host must be unix:// or tcp:// — got %q", host)
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: tr}, nil
}

func firstOrEmpty(list []string) string {
	if len(list) > 0 {
		return list[0]
	}
	return ""
}
