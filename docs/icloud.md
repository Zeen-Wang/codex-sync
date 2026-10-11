# Two Macs, two Apple Accounts, two Codex accounts

The `icloud` provider uses an **existing, dedicated iCloud Drive shared folder**
as an encrypted transport. Each Mac keeps its own Codex login, installation ID,
configuration, databases, encryption key file and sync state. Only sessions,
archived sessions, thread names, history and attachments are synchronized.
Even a hand-edited `scope: full` or `sync_paths: ["."]` cannot widen this provider.
Projects/sidebar organization, source code, worktrees and cloud-account access
are not transferred. A shared transcript can still contain secrets from prompts,
tool output or attachments; every participant with the encryption key can read it.

## Set up the shared folder

1. On Mac A, create a dedicated empty folder in iCloud Drive, such as
   `Codex Sessions`. In Finder, share it with **Only invited people** and **Can
   make changes**, explicitly inviting the Apple Account used on Mac B.
   Family Sharing membership or a shared storage plan does not itself share this
   folder. See [Apple's folder sharing instructions](https://support.apple.com/guide/mac-help/mchl91854a7a/mac).
2. On Mac B, accept that invitation. Use the actual shared folder, not a copied
   folder with the same name. The local pathname may differ on each Mac. Finder's
   Option + right-click **Copy … as Pathname** is useful. Do not hard-code the
   owner's home directory on the participant's machine.
3. Verify sharing first: create a harmless test file on A, wait until its contents
   appear on B, modify it on B and verify A receives it. Remove the test file and
   wait for both directories to be empty. Use Finder's **Keep Downloaded** option
   where available; download the entire folder before each handoff.
4. Back up both local Codex homes and `~/.codex-sync` independently. Quit Codex and
   other session writers for each real push/pull. Keep both Codex versions aligned.

Build from this checkout (`make build`). On **each** Mac run:

```sh
./bin/codex-sync init --provider icloud \
  --shared-folder "$HOME/Library/Mobile Documents/com~apple~CloudDocs/Codex Sessions"
```

Replace the example with that Mac's actual absolute path. The wizard expands a
leading `~/`; it deliberately does not discover or create the shared root. A
missing folder is an error, not an invitation to silently create an unsynced one.
It initializes a format marker only in an empty folder, or accepts an already
initialized folder. It cannot verify Apple sharing permissions, cloud delivery,
or whether a locally writable folder is actually shared.

Select **Passphrase**, using the same strong passphrase on both Macs, independently
of either Codex or Apple password. The existing age encryption and Argon2 salt
are unchanged. Alternatively transfer just the age key securely outside the
shared folder. Never share `auth.json`, `~/.codex`, or `~/.codex-sync` themselves.
The wizard checks the locally visible encrypted objects against the key; if iCloud
has not delivered any objects yet, that check cannot validate the other Mac's key.
Initialize A and wait for its first push to reach B before initializing B.

The resulting configuration on each Mac resembles:

```yaml
storage:
  provider: icloud
  shared_folder: /Users/your-local-user/Library/Mobile Documents/com~apple~CloudDocs/Codex Sessions
scope: sessions
encryption_key_path: ~/.codex-sync/age-key.txt
```

The config, key and `state.json` stay local. If switching an existing installation
from another backend, back up its config/key/state first. After setup, move the old
`state.json` aside locally so the new provider starts fresh; do not reuse another
Mac's state file. Do not use `init --passphrase` to switch providers: with an
existing config that flag only changes the key. Changing a live remote's encryption
key is unsupported; keep the original key or use a new empty dedicated folder.

## Handoff workflow

Use **one Mac at a time**, including background sync jobs. Disable existing
codex-sync launchd schedules on both Macs before adopting this transport
(`make uninstall-launchd` if installed from this repo).

1. First upload on A: `./bin/codex-sync push`.
2. Wait for Finder to finish uploading on A and downloading on B. A successful
   command means the local iCloud replica was written, not that B received it.
3. On B: `./bin/codex-sync pull --dry-run`, then `./bin/codex-sync pull`.
   Review the existing first-pull backup prompt. Use `pull --desktop` with the app
   fully quit if desktop re-indexing is needed; the existing backup and running-app
   checks still apply. Reopen Codex and verify the expected conversation before
   continuing it.
4. After working on B, quit Codex, push, wait for delivery, then pull on A before
   editing there. Sync source repositories separately; configure `path_map` when
   project locations differ (see the main README).

## Guarantees and limits

- Ciphertext revisions are immutable, hashed and retained, including ancestors.
  Publication is atomic on the local filesystem. Records include plaintext object
  names and timestamps; session contents remain encrypted. Files use mode 0600.
- Session uploads compare the locally visible revision with the last synced one.
  A stale writer is refused with a pull-first error. Normal local-vs-remote session
  conflicts retain the existing `.conflict.*` workflow.
- Missing ancestors, corrupt records, unexpected cloud conflict copies and visible
  `.icloud` placeholders fail closed. Downloaded session bytes are bound to the
  exact listed revision. A wholly missing latest revision that this Mac has never seen, or a new absent object,
  is **undetectable** from an otherwise consistent stale replica. Known missing
  revisions block rollback to their parents. Finder may also
  block a read while hydrating an evicted file; keep the folder downloaded.
- Divergent revision chains fail closed once both branches arrive. Neither clock
  order nor arrival order chooses a winner. Distinct new files can arrive in
  either order, but simultaneous metadata-manifest writes can also cause a fork.
- A lock in the Mac's local temporary directory excludes concurrent push/pull
  processes using this folder on that Mac. **There is no distributed lock**,
  cross-Mac compare-and-swap, cloud flush barrier, or transactional multi-file
  snapshot. iCloud eventual consistency cannot make simultaneous/offline writers
  safe. Local locking does not stop Codex itself from writing sessions.
- Pull never removes a local file because it is absent from this transport, even
  without `--no-delete`. `push --force`, remote reset/clear and all backend deletes
  are refused. Archived/deleted sessions can therefore remain in both locations;
  this tradeoff avoids data loss from incomplete listings.
- All revisions consume storage; there is no garbage collection. Listings check
  retained ancestry and verify new/changed records (metadata is cached within a
  run); downloads always re-check hashes. A large history increases I/O and sync time. Keep separate
  backups and periodically migrate reconciled data to a fresh shared folder.
  Do not manually prune ancestors in a live folder.

## Recovery

For incomplete downloads, wait, download the full folder in Finder, and retry.
Do not delete the reported missing parent or conflict copy to silence an error.
For a stale local lock, stop/verify all `codex-sync` processes first, then remove
**only the empty lock directory named in the error**. Locks are never stolen based
on a timeout; a crashed process may leave one behind.

For a normal `.conflict.*` file, review it and use `codex-sync conflicts`. The next
push checks the resolved remote content before adopting its revision.

For an **iCloud concurrent revisions** error:

1. Stop writers/schedules on both Macs. Back up both local homes and the complete
   shared folder. Let Finder finish delivering all available revisions.
2. The error names the key and two revision IDs. Export each to a new file outside
   the Codex home and shared folder:

   ```sh
   ./bin/codex-sync icloud export REVISION_ID --output /absolute/recovery/branch-a.jsonl
   ```

   The output is decrypted, decompressed, mode 0600 and never overwrites an existing
   file. Portable `${HOME}` / path-map tokens are retained. It may be a metadata
   manifest rather than a transcript; inspect the printed object name. This
   command also works while the folder has divergent heads and never mutates it.
3. Review/reconcile the exported data and both local homes in a separate recovery
   copy. For session JSONL, do not concatenate incompatible continuations blindly.
   Resolve portable paths for the destination and preserve both originals.
4. Use a **new empty shared folder**, retaining the old folder as recovery history.
   Back up and move aside local sync state on both Macs, configure the new folder
   with the original encryption key, push the reconciled source on A, wait for
   delivery, then preview/pull on B. Do not remove individual revision records or
   resume writes into the forked history. No automatic winner selection is offered.

## Validation

Automated tests use temporary folders and synthetic separate Codex homes. They
cover encryption/identity isolation, stale writes, retained forks, reordered
parent delivery, incomplete listings, corrupt records, invalid keys, symlinks,
local locking and forced-delete refusal. No live credentials are used.

Before treating this as your primary workflow, perform the handoff steps above on
**copies** of the two real Mac homes, signed into their distinct Apple Accounts.
Verify both directions, names, attachments, project paths, desktop visibility,
unchanged login files and recovery after an offline conflicting edit. These tests
cannot prove Apple's delivery behavior or desktop compatibility on your machines.
