# Changelog

Released versions are described in their
[GitHub releases](https://github.com/0xble/toolkit/releases).

## Unreleased

### Added

- `op.Op.Paged` (and `op.Entry.Paged`) declares an output that is a page: an
  object with an `items` array next to envelope keys such as `next_cursor`
  and `has_more`. The CLI's `--fields` then keeps the named keys of each item
  and every envelope key, so `meetings --fields id,title` no longer prints
  `{}`. Without `Paged`, `--fields` still keeps top-level keys, so no existing
  output changes. `op.Add` rejects `Paged` on an output without an `items`
  array.
