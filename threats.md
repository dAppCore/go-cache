# go-cache threat-model audit

Audit-by: Cerberus (via codex)
Repo: dappco.re/go/cache
Date: 2026-04-25

## 1. Untrusted-key DoS

Status: Complete

Question: Are key lengths bounded on the externally reachable write paths?

Finding: Yes. `Cache.Path` validates every key with `ensureSafeKey` before constructing storage paths (`cache.go:131`, `cache.go:136`). That helper rejects empty keys, keys longer than 4096 bytes, backslashes, control bytes, empty path segments, `.`, and `..` (`cache.go:743`, `cache.go:747`, `cache.go:750`, `cache.go:753`, `cache.go:757`). `Set`, `SetWithTTL`, `SetBinary`, and `SetBinaryWithTTL` all route through `entryPaths` and therefore through `Path` before writing (`cache.go:207`, `cache.go:218`, `cache.go:225`, `cache.go:230`, `cache.go:324`, `cache.go:335`, `cache.go:342`, `cache.go:346`). Regression coverage: `TestCache_ThreatUntrustedKeyDoS_RejectsOversizedKeysOnWritePaths` (`cache_test.go:2487`).

Severity: None for overlong single-key path or memory amplification via key string on those write paths.

Question: Can a flood of unique valid keys cause unbounded growth?

Finding: Yes, for storage growth inside the configured cache root. Cache entries are persisted via the configured `coreio.Medium`, and there is no entry-count or byte quota before `medium.Write` in JSON or binary writes (`cache.go:268`, `cache.go:384`, `cache.go:390`). The ordinary cache does not keep cached values in a Go map; the in-memory maps are invalidation callbacks and opened HTTP cache handles (`cache.go:48`, `cache.go:978`). A downstream consumer that forwards attacker-controlled unique valid keys can therefore grow files/inodes within `baseDir` until the backing medium or host quota stops it.

Severity: Medium. This is bounded to the configured cache root and by the underlying storage backend, but the package does not provide a built-in quota/eviction policy. No code fix was applied because adding a default global entry cap would change cache semantics and there is no existing public configuration surface for quotas in this ticket scope.

Question: Does `Invalidate` accept callback-returned glob patterns without a length backstop before `keysByPattern` lists and matches all cache keys?

Finding: Yes (prior-pass finding, retained). Validate invalidation patterns with a fixed byte limit before listing cache entries.

Severity: Medium.

Repro test: `TestCache_Invalidate_UntrustedPatternLength_Bad`.

## 2. Path traversal

Status: Complete

Question: Do disk paths derive from raw keys without sanitisation?

Finding: No for the core cache. `Path` validates the key, joins `baseDir` with `key + ".json"`, normalizes to an absolute path, and rejects paths outside the cache root prefix (`cache.go:136`, `cache.go:140`, `cache.go:141`, `cache.go:144`). Binary sidecar paths use the same validated key through `entryPaths` before writes (`cache.go:154`, `cache.go:155`, `cache.go:161`).

Severity: None for direct `../`, absolute path, control-byte, or backslash traversal through `Set`, `SetWithTTL`, `SetBinary`, `Get`, `Delete`, and related key-based operations.

Question: Do CacheStorage or HTTPCache paths derive from untrusted names or request URLs?

Finding: No direct traversal found. `CacheStorage.Open` and `CacheStorage.Delete` validate cache names before joining them under the storage base directory (`cache.go:1015`, `cache.go:1019`, `cache.go:1029`, `cache.go:1047`, `cache.go:1051`, `cache.go:1057`). The validator rejects empty names, names over 255 bytes, `/`, `\`, control bytes, `.`, and `..` (`cache.go:1066`, `cache.go:1070`, `cache.go:1073`, `cache.go:1076`, `cache.go:1079`). `HTTPCache` stores request metadata under SHA-256 hex request keys rather than raw URLs (`cache.go:1215`, `cache.go:1452`, `cache.go:1457`), and cached response body reads validate that `BodyPath` is a relative `responses/<key>.bin` path with safe segments (`cache.go:1407`, `cache.go:1410`, `cache.go:775`, `cache.go:789`, `cache.go:800`).

Severity: None for reviewed raw-name and raw-URL path traversal.

Question: Does ScopedCache origin namespacing allow path injection?

Finding: No. Scope prefixes are `scope_` plus a SHA-256 hex digest of the origin string, so raw origins are not embedded in file paths (`cache.go:711`, `cache.go:714`, `cache.go:814`, `cache.go:815`, `cache.go:817`). Scoped keys are prefixed and then passed back through the parent cache validation and path containment checks (`cache.go:820`, `cache.go:835`, `cache.go:839`). Regression coverage: `TestCache_ThreatPathTraversal_ScopedOriginIsHashedAndKeysStillValidated` (`cache_test.go:2534`).

Severity: None for origin-derived path traversal.

Question: Do HTTPCache request URLs become path components?

Finding: No. HTTP request storage keys are SHA-256 hex digests of `method + NUL + URL` (`cache.go:1215`, `cache.go:1452`, `cache.go:1457`), and `Put` writes metadata/body under `responses/<digest>.json` and `responses/<digest>.bin` (`cache.go:1343`, `cache.go:1347`, `cache.go:1362`, `cache.go:1363`, `cache.go:1383`, `cache.go:1388`). Regression coverage: `TestCache_ThreatPathTraversal_HTTPCacheUsesHashedRequestStorageKeys` (`cache_test.go:2557`).

Severity: None for raw-URL path traversal on the reviewed HTTPCache write path.

Question (prior pass): Symlink-following inside the cache root?

Finding: A symlinked directory or file already under `baseDir` could redirect an otherwise safe key outside the cache root. Reject existing symlink components from the cache root through the resolved cache path before returning paths for filesystem use.

Severity: high.

Repro test: `TestCache_Path_PathTraversalSymlink_Bad`.

## 3. Eviction TOCTOU

Status: Complete

### 3.1 Invalidate map walk / OnInvalidate registration

Status: Complete

Question: What lock is held while `Invalidate` walks callbacks, and can `OnInvalidate` append to the same trigger while that walk is in progress?

Finding: No map-walk race found. `Cache.mu` is the lock protecting the invalidation callback map (`cache.go:50`). `OnInvalidate` takes the write lock before appending to `cache.invalidation[trigger]` (`cache.go:696`, `cache.go:698`). `Invalidate` takes the read lock only long enough to copy the trigger's callback slice, then releases the lock before executing callbacks and deleting entries (`cache.go:709`, `cache.go:710`, `cache.go:711`, `cache.go:713`). A callback that registers more invalidations therefore cannot mutate the map while it is being read. The newly registered callback is not included in the already-snapshotted invalidation pass, which is acceptable snapshot semantics. No `delete(c.invalidation, trigger)` call exists in the reviewed cache implementation.

Severity: None.

### 3.2 TTL expiry race on Get

Status: Complete

Question: Can two concurrent readers of a freshly expired entry return expired data, or does one reader delete/alter state out from under the other?

Finding: No unsafe TTL expiry race found. `Get` reads the entry under the entry read lock, unmarshals the cache envelope, checks `time.Now().After(entry.ExpiresAt)`, and returns `found=false` before unmarshalling cached data into the caller's destination (`cache.go:178`, `cache.go:186`, `cache.go:195`, `cache.go:200`, `cache.go:201`, `cache.go:204`). `GetBinary` follows the same metadata-first expiry check and returns `found=false` before reading the payload body (`cache.go:439`, `cache.go:445`, `cache.go:446`, `cache.go:449`). Expired reads do not delete files, so two readers can both lose and safely return not-found; neither path returns expired data after observing the expiry check.

Severity: None.

### 3.3 Get-then-Set caller-site TOCTOU

Status: Complete

Question: If two consumers both observe `Get` as missing or expired and then both call `Set`, does `Cache.mu` serialize the writes, or is this just last-writer-wins cache behavior?

Finding: Yes, fixed. `Cache.mu` protects invalidation callback registration and snapshotting, while `entryMu` now serializes cache entry I/O separately (`cache.go:50`, `cache.go:51`). `Get` and `GetBinary` take `entryMu.RLock` while reading entries (`cache.go:178`, `cache.go:423`). `Set` and `SetBinary` take `entryMu.Lock` across path resolution, rollback snapshot, and writes (`cache.go:238`, `cache.go:246`, `cache.go:279`, `cache.go:280`, `cache.go:360`, `cache.go:368`, `cache.go:401`, `cache.go:407`). Delete paths are also serialized: single-key removal locks before deleting metadata and binary sidecars, `DeleteMany` locks across its batch, and invalidation pattern listing takes the entry read lock while walking keys (`cache.go:306`, `cache.go:315`, `cache.go:323`, `cache.go:469`, `cache.go:486`, `cache.go:550`, `cache.go:553`). Pure cache freshness remains last-writer-wins, but callers no longer need the backing `coreio.Medium` to tolerate overlapping entry operations. Regression coverage: `TestCache_ThreatTOCTOU_GetThenSetSerializesEntryWrites` (`cache_test.go:2668`).

Severity: Medium before fix; mitigated by entry-level serialization.
