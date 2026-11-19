# Backup & Restore

All durable state lives in the PostgreSQL volume (`pgdata`) — there is nothing else to back up.

## Backup

**Online dump (recommended, safe while running):**

```bash
cd deploy
docker compose exec db pg_dump -U labvory -d labvory -Fc > labvory-$(date +%F).dump
```

**Volume snapshot (cold):** stop compose, copy the volume, start again:

```bash
cd deploy
docker compose stop backend frontend
docker run --rm -v labvory_pgdata:/data -v "$PWD":/out alpine \
  tar czf /out/pgdata-$(date +%F).tar.gz -C /data .
docker compose start backend frontend
```

## Restore

```bash
cd deploy
docker compose exec -T db pg_restore -U labvory -d labvory --clean --if-exists < labvory-2026-11-16.dump
docker compose restart backend
```

From a volume snapshot:

```bash
cd deploy
docker compose down -v   # destroys the current volume
docker volume create labvory_pgdata
docker run --rm -v labvory_pgdata:/data -v "$PWD":/in alpine \
  sh -c "cd /data && tar xzf /in/pgdata-2026-11-16.tar.gz"
docker compose up -d
```

## Verification

After any restore, confirm:

1. `curl http://localhost:8088/healthz` → `{"status":"ok","database":"ok"}`
2. Sign in with the existing administrator.
3. Activity page shows the pre-backup audit trail (proves data integrity end to end).

Schedule the `pg_dump` command via cron on the Docker host; keep dumps off the same disk as the
volume. Test restores periodically — a backup you have not restored is a hypothesis.

## Built-in scheduled backups

LabVory can also snapshot the inventory itself on a schedule: **Settings → Scheduled backups**
enable JSON snapshots written to disk on an interval (default 24 h), with a retention count
(default 14) that prunes the oldest files. Each run is recorded in the Activity log, and a failed
run pushes a webhook alert if notifications are configured.

- Files use the same document shape as **Import & export → Export JSON**, so any snapshot
  restores losslessly via that page.
- The directory is relative to the server process (`./backups` by default). In Docker, compose
  already mounts the `backups` volume at `/app/backups`; on a bare binary, point the setting at a
  durable path or snapshots die with the host directory.
- Snapshots are a convenience copy of the inventory — for full data safety (sessions, audit
  trail, observed metadata) the `pg_dump` above remains the recommended primary backup.

To restore a snapshot: **Import & export → Restore**, upload the JSON file. Restore is additive
and idempotent — it recreates missing items and leaves existing data untouched.
