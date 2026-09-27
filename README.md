# fat — read-only FAT12/16/32 forensic parser

Parses a single-volume raw dd image (byte 0 = boot sector) per the
Microsoft FAT specification. Never modifies the image, never loads it
into memory, and treats every on-disk structure as untrusted input.

## API

```go
v, err := fat.Open(path)          // parse BPB: FAT type, cluster size/count
err = v.Walk(func(e *fat.Entry) error { ... })  // every dir entry, incl. deleted
err = v.ReadFile(entry, w)        // stream content; deleted files carved contiguously
```

## Properties

- FAT type is derived **only** from the data-region cluster count
  (<4085 FAT12, <65525 FAT16, else FAT32). `BS_FilSysType` and the
  zero/non-zero pattern of BPB fields are never consulted.
- LFN/short-entry pairing: live entries require adjacent, sequentially
  numbered, checksum-matching live LFNs; deleted entries pair with
  adjacent deleted LFNs (ordinals overwritten by 0xE5) whose checksums
  match the short name once its first character is restored from the
  long name. Ungroupable LFNs are discarded.
- Deleted entries: first short-name character restored from the LFN
  (group invalid if no legal short-name character can be derived), or
  `_` when no LFN exists. NTRes case flags lower-case names without LFNs.
  First byte 0x05 means a literal 0xE5; 0x00 ends a directory.
- Deleted-file recovery status: `full` (all needed contiguous clusters
  free), `partial-overwrite` (some reallocated), `unrecoverable`
  (first cluster invalid or range past the volume end).
- Deleted directories are entered only when the first cluster is free
  and starts with `.`/`..` pointing to self/parent; only that cluster is
  parsed and everything inside is reported as deleted.
- Timestamps are volume-local, no timezone conversion, 10ms creation
  resolution. Zero or out-of-range fields yield an empty timestamp —
  never 1980-01-01, never library-normalized values.
- Corruption (chain loops, ancestor-pointing directories, out-of-range
  clusters, absurd sizes, truncated images) becomes per-entry anomaly
  annotations or errors; other entries are still produced. No panics,
  no hangs, no corruption-driven allocations. Cluster chains are walked
  with Brent cycle detection in constant memory.
- Allocated chains that are too short/long for the size field, or pass
  through free/bad clusters, are annotated; `ReadFile` emits only bytes
  actually reachable on the chain and within the size field.

## Tests

All images are built byte-by-byte in test code. Run:

```sh
go test ./...
go test -fuzz=FuzzOpenWalk -fuzztime=60s -run=^$ .
```
