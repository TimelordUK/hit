# Chocolatey packaging

The package sources for `choco install hit`. Modelled on sql-cli's, which the Chocolatey
moderators accepted, with the changes they asked for applied: no `VERIFICATION.txt` or
`LICENSE.txt` (the package downloads the binary rather than embedding it), and a
`packageSourceUrl` pointing here.

## Layout

- `hit.nuspec.template`: package metadata. `__VERSION__` is filled in at pack time.
- `tools/chocolateyInstall.ps1.template`: downloads `hit_v<version>_windows_amd64.zip`
  from the matching GitHub release, checks its SHA256 and unpacks `hit.exe`, which
  Chocolatey shims onto PATH. `__VERSION__` and `__CHECKSUM64__` are filled in at pack time.
- `tools/chocolateyBeforeModify.ps1`: before an upgrade or uninstall, stops any
  `hit serve` running from this package, since it holds `hit.exe` open.

The package never edits `$PROFILE`; its description and install message say which line
to add.

## Publishing: by hand, never on tag

Moderating one version can take weeks or months, and a version waiting in review blocks
pushes, so publishing is kept out of the tag release (`release.yml`) entirely. To send a
version:

1. Actions → **chocolatey** → Run workflow, with an existing tag (`v0.1.17`).
2. Leave **push** off first: the packed `.nupkg` is kept as an artifact to try.
3. Run again with **push** on to submit it. Needs the `CHOCOLATEY_API_KEY` secret.

The first submission of a new package id goes to manual review; later versions usually
pass the automated checks and are approved sooner.

## Testing locally

```powershell
./scripts/choco-pack.ps1 -Tag v0.1.17               # -> dist/choco/hit.0.1.17.nupkg
choco install hit -s dist/choco -y --force          # in an admin shell
hit version
choco uninstall hit -y
```

`choco-pack.ps1` takes the checksum from the release's `SHA256SUMS.txt` and checks it
against the zip it names, so a package cannot point at bytes other than the ones
published.
