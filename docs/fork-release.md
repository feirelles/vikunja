# Fork release builds (tasks.felipemeirelles.com.br)

Production runs upstream's official binary. To ship fork fixes before upstream releases them,
we build from **an upstream tag plus cherry-picked patches**, never from `main`.

## Branches

- `release/v2.7.1-fork`: created from upstream tag `v2.7.1`. Only cherry-picks of patches that
  are already merged in the fork (`git cherry-pick -x <sha>`), one commit per patch.
- A new upstream release gets a new branch (`release/vX.Y.Z-fork` from tag `vX.Y.Z`); re-apply the
  patches upstream has not merged yet and drop the rest.
- **No patch with a migration** (`pkg/migration/`) goes on a release branch without an explicit
  decision: a migration makes the database newer than the official binary, so going back would
  need a dump restore instead of a binary swap.

## Build

```sh
git switch release/v2.7.1-fork   # clean tree; use a worktree if main is checked out elsewhere
scripts/build-fork.sh            # OUT_DIR=... to change the output dir (default dist/fork)
```

The script installs the toolchain pinned in the tag's `mise.toml` (`mise install`), builds the
frontend (`pnpm install --frozen-lockfile && pnpm build`), then `mage build:build`, and copies the
binary to `dist/fork/vikunja-<version>-linux-amd64` with a `.sha256` (the commit is part of the
version once patches are on top; on the bare tag the file name gets `-<sha>` appended).

The version inside the binary is `git describe --tags`: `v2.7.1` on the bare tag,
`v2.7.1-<n>-<sha>` once patches are on top. `vikunja restore` only accepts a dump made by the same
version, so keep every binary that ran in production next to its dumps.

Measured on the `claude` VM (8 vCPU, 31 GiB RAM), 2026-11-09, Go module cache cold:

| | |
|---|---|
| wall time (frontend + API) | 1 min 50 s (`/usr/bin/time -v`; 284 s user, 59 s sys, 311 % CPU) |
| peak RSS, largest single process | 1.52 GiB (1 594 288 kB) |
| system RAM in use during build | +4.5 GiB over the idle baseline (sampled every 2 s) |
| binary size | 104 MB (99.2 MiB); the official release binary is 66 MB because it is UPX-packed |

## Test instance (before every production switch)

Never touch `~/.local/share/vikunja`, `~/.config/vikunja/config.yml` or port 3456.
Port 3457 is `tasks-webhook.service`; use **3460**.

```sh
T=~/tmp/vikunja-test; mkdir -p "$T" && chmod 700 "$T"
# online-safe copy of the live SQLite DB (WAL), read-only on the source
python3 -I -c "import sqlite3,sys; s=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True); d=sqlite3.connect(sys.argv[2]); s.backup(d); print(d.execute('PRAGMA integrity_check').fetchone())" \
  ~/.local/share/vikunja/vikunja.db "$T/vikunja.db"
cp -a ~/.local/share/vikunja/files "$T/files"
```

`$T/config.yml`: `service.interface: 127.0.0.1:3460`, `service.rootpath`, `database.path`,
`files.basepath` pointing at `$T`, a fresh `service.secret`, and **`webhooks.enabled: false`**,
`mailer.enabled: false` (the copied webhooks point at the live dispatcher). Then:

```sh
dist/fork/vikunja-<ver> --config "$T/config.yml" &           # runs migrations on the copy
curl -s http://127.0.0.1:3460/api/v1/info
dist/fork/vikunja-<ver> --config "$T/config.yml" user create -u smoke -e smoke@example.invalid -p '<pw>'
curl -s -X POST http://127.0.0.1:3460/api/v1/login -H 'Content-Type: application/json' \
  -d '{"username":"smoke","password":"<pw>"}'                 # expect a token
kill %1; wait   # graceful shutdown takes a few seconds
rm -rf "$T"
```

## Production switch (Felipe triggers it; not automated)

Binaries live versioned next to the one in use, so going back is a rename:

```sh
B=~/.local/bin; NEW=dist/fork/vikunja-<ver>-<sha>-linux-amd64
sha256sum -c "$NEW.sha256"
"$B/vikunja" version                                  # note what runs now
systemctl --user start vikunja-dump.service           # fresh dump, named after the running version
ls -t ~/backups/vikunja | head -1                     # check it exists and is non-empty
cp -n "$B/vikunja" "$B/vikunja-v2.7.1-official"       # keep the official binary (once)
install -m 755 "$NEW" "$B/vikunja-<ver>"
systemctl --user stop vikunja.service
ln -sfn "$B/vikunja-<ver>" "$B/vikunja"
systemctl --user start vikunja.service
journalctl --user -u vikunja.service -n 50 --no-pager    # migrations, no errors
curl -s http://127.0.0.1:3456/api/v1/info                # "version" = <ver>
curl -sI https://tasks.felipemeirelles.com.br/ | head -1 # through cloudflared
~/.local/bin/veans list --mine                           # API through a bot token
```

After the switch the nightly `vikunja-dump.sh` names dumps after the fork version.

## Back to the official binary

Without migrations in the patches the database schema is the same, so swapping the binary back
is enough:

```sh
systemctl --user stop vikunja.service
ln -sfn ~/.local/bin/vikunja-v2.7.1-official ~/.local/bin/vikunja
systemctl --user start vikunja.service
curl -s http://127.0.0.1:3456/api/v1/info                # "version" = v2.7.1
```

If a patch did add a migration: stop the service, move the data dir aside, and restore the last
dump taken **before** the switch with the official binary
(`vikunja --config ~/.config/vikunja/config.yml restore <dump.zip>`); restore refuses a dump from
another version. Everything written after that dump is lost.
