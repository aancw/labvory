package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/labvory/internal/store"
)

// SyncICMP performs opt-in reachability checks against an explicit asset
// allowlist (spec §4: network actions require explicit scope). Results land in
// metadata.observed — the operator-declared status field is never touched.
func SyncICMP(ctx context.Context, pool *pgxpool.Pool, integ store.Integration) (map[string]any, error) {
	var cfg struct {
		AssetIDs []string `json:"asset_ids"`
		TimeoutS int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if len(cfg.AssetIDs) == 0 {
		return nil, fmt.Errorf("icmp scope is empty: add asset ids to config.asset_ids (opt-in by design)")
	}
	if cfg.TimeoutS <= 0 {
		cfg.TimeoutS = 2
	}

	assets := store.NewAssets(pool)
	integrations := store.NewIntegrations(pool)

	reachable, unreachable := 0, 0
	for _, assetID := range cfg.AssetIDs {
		if !isUUIDLike(assetID) {
			return nil, fmt.Errorf("invalid asset id in scope: %s", assetID)
		}
		asset, err := assets.GetByID(ctx, assetID)
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", assetID, err)
		}
		target := asset.Hostname
		if target == "" && asset.PrimaryIP != nil {
			target = *asset.PrimaryIP
		}
		if target == "" {
			unreachable++
			_ = integrations.SetObservedSub(ctx, assetID, "icmp", map[string]any{
				"status": "no-target", "checked_at": time.Now().UTC().Format(time.RFC3339),
			})
			continue
		}

		ok := ping(ctx, target, cfg.TimeoutS)
		if ok {
			reachable++
		} else {
			unreachable++
		}
		state := "offline"
		if ok {
			state = "online"
		}
		// The sub-key variant leaves other observers (ARP sightings, the
		// manual check-now action) untouched — never use the clobbering
		// whole-map SetObserved here.
		if err := integrations.SetObservedSub(ctx, assetID, "icmp", map[string]any{
			"status": state, "target": target, "checked_at": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return nil, err
		}
	}

	return map[string]any{"checked": len(cfg.AssetIDs), "reachable": reachable, "unreachable": unreachable}, nil
}

// ProbeHost runs one ICMP check against a host (hostname or IP). Exported for
// the on-demand "check now" asset action; the scheduled connector and it
// share the same probe and observation shape.
func ProbeHost(ctx context.Context, host string, timeoutS int) bool {
	return ping(ctx, host, timeoutS)
}

// ping shells out to the system ping binary (present on alpine and macOS).
// Reachable means the process exited 0 — a fast "100% packet loss" exit is a
// negative answer, not a timeout.
func ping(ctx context.Context, host string, timeoutS int) bool {
	timeout := time.Duration(timeoutS+1) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "ping", "-c", "1", "-W", fmt.Sprintf("%d", timeoutS), host)
	return cmd.Run() == nil
}

func isUUIDLike(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}
