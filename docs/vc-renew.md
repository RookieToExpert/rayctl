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

## Conservative Deletion

The manifest `.rayctl-vc-renew.json` tracks only files created by this command.
It records resource identity and the file hash, not credentials. Unmanaged
legacy files, symlinks, and modified files are never deleted.

Absence from a resource list, HTTP 401/403/404, timeout, and an inaccessible
profile are not proof of deletion. Cleanup currently requires a successful
detail response with the same UID and an explicit `DELETED` state. If the
platform permanently removes records and returns only 404, cleanup deliberately
keeps those files: an authoritative deletion API or independent inventory would
be needed to safely support that case.

Eligible files are displayed for confirmation `(y/N)`. Only `y` deletes them
permanently; no backup is created. File hashes are rechecked before deletion.
Partial inventory/download failures are reported and result in a nonzero exit
status, without removing affected local files.
