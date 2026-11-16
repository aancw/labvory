package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/secrets"
	"github.com/aancw/lanvory/internal/store"
)

// Runner executes integration syncs and records their history.
type Runner struct {
	pool *pgxpool.Pool
	box  *secrets.Box
}

func NewRunner(pool *pgxpool.Pool, box *secrets.Box) *Runner {
	return &Runner{pool: pool, box: box}
}

// Run performs one sync for the given integration and records the outcome.
// A sync failure never crashes the server: it is captured in the run history.
func (r *Runner) Run(ctx context.Context, integ store.Integration) (map[string]any, error) {
	integrations := store.NewIntegrations(r.pool)
	runID, err := integrations.StartRun(ctx, integ.ID)
	if err != nil {
		return nil, fmt.Errorf("start sync run: %w", err)
	}

	summary, syncErr := r.dispatch(ctx, integ)

	status := "success"
	message := ""
	if syncErr != nil {
		status = "failed"
		message = syncErr.Error()
	}

	summaryJSON, _ := json.Marshal(summary)
	if summary == nil {
		summaryJSON = []byte("{}")
	}
	// Bookkeeping must survive client disconnects: a canceled request context
	// would otherwise leave the run stuck in "running".
	book := context.WithoutCancel(ctx)
	if err := integrations.FinishRun(book, runID, status, message, summaryJSON); err != nil {
		r.log().Error("finish sync run failed", "error", err, "run", runID)
	}
	if err := integrations.TouchSync(book, integ.ID); err != nil {
		r.log().Warn("touch sync failed", "error", err)
	}

	return summary, syncErr
}

func (r *Runner) dispatch(ctx context.Context, integ store.Integration) (map[string]any, error) {
	switch integ.Kind {
	case "proxmox":
		secret, err := r.secretValue(integ, "token_secret")
		if err != nil {
			return nil, err
		}
		return SyncProxmox(ctx, r.pool, integ, secret)
	case "docker":
		var cfg struct {
			AssetID string `json:"asset_id"`
		}
		if err := json.Unmarshal(integ.Config, &cfg); err != nil {
			return nil, err
		}
		if cfg.AssetID == "" {
			return nil, fmt.Errorf("docker integration needs config.asset_id (the host asset containers attach to)")
		}
		return SyncDocker(ctx, r.pool, integ, cfg.AssetID)
	case "icmp":
		return SyncICMP(ctx, r.pool, integ)
	case "arp":
		return SyncARP(ctx, r.pool, integ)
	case "dns":
		return SyncDNS(ctx, r.pool, integ)
	case "cloudflare":
		secret, err := r.secretValue(integ, "api_token")
		if err != nil {
			return nil, err
		}
		return SyncCloudflare(ctx, r.pool, integ, secret)
	case "kubernetes":
		// The token secret is optional: kubeconfig or in-cluster auth may
		// already carry credentials.
		secret, err := r.secretValueOptional(integ, "token")
		if err != nil {
			return nil, err
		}
		return SyncKubernetes(ctx, r.pool, integ, secret)
	case "adguard":
		secret, err := r.secretValue(integ, "password")
		if err != nil {
			return nil, err
		}
		return SyncAdGuard(ctx, r.pool, integ, secret)
	default:
		return nil, fmt.Errorf("integration kind %q has no sync implementation", integ.Kind)
	}
}

// secretValue decrypts a named secret from the integration config.
func (r *Runner) secretValue(integ store.Integration, key string) (string, error) {
	if r.box == nil {
		return "", fmt.Errorf("LV_SECRET_KEY is not configured on the server: this integration needs stored credentials")
	}
	var cfg struct {
		Secrets map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return "", err
	}
	value := cfg.Secrets[key]
	if value == "" {
		return "", fmt.Errorf("secret %q is not configured", key)
	}
	plain, err := r.box.Decrypt(value)
	if err != nil {
		return "", fmt.Errorf("decrypt secret %q: %w", key, err)
	}
	return plain, nil
}

// secretValueOptional returns "" when the secret is simply not configured.
func (r *Runner) secretValueOptional(integ store.Integration, key string) (string, error) {
	var cfg struct {
		Secrets map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return "", err
	}
	value := cfg.Secrets[key]
	if value == "" {
		return "", nil
	}
	if r.box == nil {
		return "", fmt.Errorf("LV_SECRET_KEY is not configured on the server: this integration needs stored credentials")
	}
	plain, err := r.box.Decrypt(value)
	if err != nil {
		return "", fmt.Errorf("decrypt secret %q: %w", key, err)
	}
	return plain, nil
}

func (r *Runner) log() *slog.Logger {
	return slog.Default()
}

var _ = time.Now
