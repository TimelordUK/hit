# hit — notes for Claude

- Read `docs/DESIGN.md` before changing behaviour; keep it in sync when decisions change.
- New ideas go in `docs/WISHLIST.md` with the next free ID in their category (`C-` core,
  `S-` shell, `F-` feature). Never renumber; mark items `dropped` instead of deleting them.
- Reference wishlist IDs in commit messages (e.g. `C-001: tolerant JSONL reader`).
- The stored command is never modified. Formatting is a view or insert option only.
- Nothing on the prompt hot path may spawn a process, touch the network, or stat a remote path.
- The owner dogfoods on pwsh 7 / Windows daily; ergonomics decided by use beat theory.
