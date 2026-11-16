# Backup & Restore

All durable state lives in the PostgreSQL volume (`pgdata`) — there is nothing else to back up.

## Backup

**Online dump (recommended, safe while running):**

```bash
cd deploy
docker compose exec db pg_dump -U lanvory -d lanvory -Fc > lanvory-$(date +%F).dump
```

**Volume snapshot (cold):** stop compose, copy the volume, start again:

```bash
cd deploy
docker compose stop backend frontend
docker run --rm -v lanvory_pgdata:/data -v "$PWD":/out alpine \
  tar czf /out/pgdata-$(date +%F).tar.gz -C /data .
docker compose start backend frontend
```

## Restore

```bash
cd deploy
docker compose exec -T db pg_restore -U lanvory -d lanvory --clean --if-exists < lanvory-2026-11-16.dump
docker compose restart backend
```

From a volume snapshot:

```bash
cd deploy
docker compose down -v   # destroys the current volume
docker volume create lanvory_pgdata
docker run --rm -v lanvory_pgdata:/data -v "$PWD":/in alpine \
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
