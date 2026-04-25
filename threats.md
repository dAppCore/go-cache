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

Question: Did the class 1 or 2 audit expose overlapping eviction TOCTOU findings?

Finding: No overlapping finding. The audit focus was untrusted-key DoS and path traversal per Mantis #923. Invalidation callback registration is protected by `Cache.mu`, and `Invalidate` copies the callback slice under an `RLock` before executing callbacks without holding the lock (`cache.go:666`, `cache.go:667`, `cache.go:679`, `cache.go:680`, `cache.go:681`). Entry deletion is idempotent with missing files ignored by the lower delete helpers (`cache.go:291`, `cache.go:301`, `cache.go:309`, `cache.go:692`, `cache.go:693`). Broader stale-read or concurrent Get/Invalidate semantics remain in sibling Mantis #924.

Severity: Not assessed beyond overlap.
