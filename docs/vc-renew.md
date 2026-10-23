# VC Kubeconfig Synchronization

```bash
rayctl vc renew --dry-run
rayctl vc renew
rayctl vc renew -e all
rayctl vc renew -e pt --skip-connectivity-check
```

The default environment follows the global environment selection. Files are
stored under `$HOME/D`, `$HOME/PT`, and `$HOME/Dcloud`. `--directory` selects
another existing root directory. Credentials are retrieved using the resource's
profile, subscription, resource group and region. This downloads existing
certificates; it does not issue or rotate certificates on the platform.

Existing files are never overwritten, including expired or invalid configs.
New files are validated for certificate/key consistency, validity dates, CA
format, and (unless disabled) TLS connectivity to `/version`. New files use mode
0600 and an atomic no-overwrite publication. No credentials are printed.

`--dry-run` does not retrieve credentials, create directories, update the
manifest, or delete files. A normal run holds an exclusive lock file. If a
process is forcibly terminated, check that no renewal process remains before
removing the stale `.rayctl-vc-renew.json.lock` file in the output root.

## Cleanup from the Platform Inventory

The manifest `.rayctl-vc-renew.json` tracks only files created by this command.
It records resource identity and the file hash, not credentials. Manifest
registration is not required for cleanup.

After the entire platform inventory for the selected environments succeeds,
local `vc-*` kubeconfig files absent from that inventory become removal
candidates. This includes legacy/unmanaged files and files modified before the
run; an explicit `DELETED` detail response is not required. Cleanup is limited
to ordinary files in the selected environment directories (`D`, `PT`, and
`Dcloud`) beneath the output root. Symlinks, directories, and unsafe paths are
not removed. Only local kubeconfig files are removed; platform VCs are not
deleted.

Eligible files are displayed for confirmation `(y/N)`. Only `y` deletes them
permanently; no backup is created. Each candidate's hash is captured when it is
selected and rechecked before deletion. A file changed during confirmation is
preserved. `--dry-run` displays candidates without prompting or deleting them.

Any failed or incomplete inventory prevents all cleanup in that run, while
successful additions may still be retained. Credential download or validation
failures do not make a successful inventory incomplete and do not prevent
cleanup of other stale files. Failures are reported and result in a nonzero
exit status.
