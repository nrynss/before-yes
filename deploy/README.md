# Deploying Before Yes

Before Yes runs as one Docker container on the shared Hetzner box, behind
Traefik, like the other apps there. The container holds the Go binary and the
built web app. Its data lives in a Docker volume, `beforeyes-data`, mounted at
`/data`. The model calls go to Token Factory and Tavily; the box does no
model work.

Run every command from the repository root, on your workstation, unless a step
says otherwise.

## 1. Set the origin address (once, on your workstation)

The box's IP address is not stored in the repository. Put it in a file that git
ignores:

```sh
cp deploy/origin.env.example deploy/origin.env
# edit deploy/origin.env and replace <origin-ip> with the box's address
```

`deploy/origin.env` is gitignored. Do not commit it.

## 2. Make the host env file (once, on the box)

The keys live in a file on the box, never in the image or the repository. Log in
as root and create `/opt/before-yes/.env.deploy` with **these key names**, one
per line, no quotes and no `export`:

| Key | What it is |
|---|---|
| `NEBIUS_KEY` | Token Factory key, used for the reader and the judge |
| `TAVILY_KEY` | Tavily key, used to find review sources |

Optional, only if you want the gate passcode turned on: `BEFORE_YES_PASSCODE`.
(It is off by default; see the note in `deploy/before-yes.prod.toml`.)

Then lock it down:

```sh
mkdir -p /opt/before-yes
nano /opt/before-yes/.env.deploy       # paste the key values yourself
chmod 600 /opt/before-yes/.env.deploy
stat -c '%a %n' /opt/before-yes/.env.deploy   # must print: 600
```

The deploy script refuses to run unless the file exists with mode 600.

## 3. Deploy

Confirm the hostname when asked (it defaults to `beforeyes.nryn.dev`):

```sh
deploy/deploy.sh
```

The script:
1. refuses a dirty git tree, then builds the image tagged `before-yes:<commit>`;
2. copies the image to the box with `docker save | docker load` (no registry);
3. runs it with `tenant.sh`, on the `proxy` network, with `--env-file` and the volume;
4. checks `/healthz` on the box.

## 4. DNS record (Cloudflare, once)

In the Cloudflare dashboard for the `nryn.dev` zone, add:

- Type `A`, name `beforeyes`, content = the origin IP (the value in `deploy/origin.env`), **Proxied**.

Traefik issues the certificate on the first request, so the site may show a
certificate error for a minute or two after the record is live.

## 5. Seed the demo cards (once, after the first deploy)

```sh
deploy/seed.sh
```

This loads the three demo cards (Roblox, Minecraft, Khan Academy Kids) from the
image into the card store. It makes no model or search call and spends nothing.
Running it again changes nothing. Check them at `https://beforeyes.nryn.dev/api/cards`.

## 6. Health check

From your workstation:

```sh
curl -s https://beforeyes.nryn.dev/healthz
curl -s -o /dev/null -w '%{http_code}\n' https://beforeyes.nryn.dev/
```

`/healthz` should answer, and the home page should return `200`.

## 7. Nightly backup

The container runs `before-yes backup`, which copies both databases
(`before-yes.db` for cards and the rest, `spend.db` for the spend meter) into
`/data/backups/` using SQLite's own backup, then keeps the 14 newest copies of
each. Run it nightly from cron on the box:

```sh
cat > /etc/cron.d/before-yes-backup <<'EOF'
15 3 * * * root docker exec beforeyes /app/before-yes backup -keep 14 >> /var/log/before-yes-backup.log 2>&1
EOF
chmod 644 /etc/cron.d/before-yes-backup
```

Test it once by hand: `docker exec beforeyes /app/before-yes backup`, then
`docker exec beforeyes ls -l /data/backups`.

## 8. Redeploy

Commit your change, then run `deploy/deploy.sh` again. The volume keeps the
data. The container is replaced.

## 9. Roll back

Each deploy leaves its image on the box. To run an earlier commit, use its tag:

```sh
docker images --format '{{.Repository}}:{{.Tag}}' | grep before-yes   # on the box
deploy/deploy.sh beforeyes.nryn.dev before-yes:<earlier-commit>
```

A rollback does not change the data. If a bad deploy also wrote bad data, restore
from a backup (step 10) as well.

## 10. Restore from a backup

Stop the app first so nothing writes during the copy. Choose the copy to restore
from `docker exec beforeyes ls /data/backups` (names start `before-yes-` and `spend-`).

```sh
docker stop beforeyes
docker run --rm --user 65532:65532 -v beforeyes-data:/data alpine sh -c '
  cp /data/backups/before-yes-<stamp>.db /data/before-yes.db &&
  cp /data/backups/spend-<stamp>.db /data/spend.db &&
  rm -f /data/before-yes.db-wal /data/before-yes.db-shm /data/spend.db-wal /data/spend.db-shm'
docker start beforeyes
curl -s https://beforeyes.nryn.dev/healthz
```

Restore only the file pairs you need. Use the same stamp for both files so the
cards and the meter agree.

## Where things are

- `Dockerfile`: the image (web build, Go build, distroless runtime, non-root).
- `deploy/before-yes.prod.toml`: production settings, baked into the image.
- `deploy/deploy.sh`: build, copy, run. `deploy/seed.sh`: load the demo cards.
- `deploy/origin.env`: your origin address (gitignored; see the example file).
