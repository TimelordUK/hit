# hit

**hi**story **t**ool: a fast, multi-line-aware shell history and directory finder for
PowerShell and zsh.

> You never lose something you typed, and you never lose somewhere you went.

**Status:** design phase. Nothing to install yet.

## Why another history tool?

- **Multi-line commands are first class.** A 4-line `Invoke-RestMethod` to Elasticsearch comes
  back exactly as you typed it, and long one-liners can be tidied into one argument per line
  using PowerShell's own parser.
- **No database.** History is a plain append-only JSONL file you can grep, sync, and query.
  There's no "can't open connection to db".
- **Network shares are first class.** Directory jumping never stats remote paths, so `\\server\share`
  entries stay fast and don't get pruned.
- **Prod failsafes.** Optional rules that warn, ask for confirmation, or block commands aimed at things
  you really don't want to hit by accident.
- **PowerShell first, zsh second.** Windows and Linux.

## Docs

- [Design](docs/DESIGN.md)
- [Wishlist](docs/WISHLIST.md) (`C-` core · `S-` shell · `F-` features)

## License

MIT
