# go-cache threat-model audit

Audit-by: Cerberus (via codex)
Repo: dappco.re/go/cache
Date: 2026-04-25

## 1. Untrusted-key DoS
**Question:** Does every public method (Set, Get, Delete, SetWithTTL, SetBinary, SetBinaryWithTTL, GetBinary, DeleteMany, Clear, ClearScope, Path, Scoped, OnInvalidate, Invalidate) reach `ensureSafeKey` before touching the filesystem? Are there backstops on key length, ScopedCache prefix escape, glob-pattern blowup?
**Finding:** YES - cache keys are bounded and public key-taking methods route through `Path`/`entryPaths` before filesystem access, and scoped prefixes are hashed plus revalidated. However, `Invalidate` accepted callback-returned glob patterns without a length backstop before `keysByPattern` listed and matched all cache keys.
**Severity:** medium
**Repro test:** TestCache_Invalidate_UntrustedPatternLength_Bad
**Fix:** validate invalidation patterns with a fixed byte limit before listing cache entries.

## 2. Path traversal
**Question:** What bytes does `hasPathDangerousBytes` cover (null byte, .., leading / or ~, control chars, URL-encoded %2e%2e)? Does symlink-following escape baseDir? Is `ensureSafeResponseBodyPath` rigour applied to JSON entry paths too?
**Finding:** YES - `ensureSafeKey` rejects empty keys, `..` segments, leading `/` via empty segments, backslashes, null/control bytes, and overlong keys; URL-encoded `%2e%2e` remains a literal safe filename segment. `ensureSafeResponseBodyPath` is applied to cached HTTP response metadata on read and write. The gap was local symlink following: a symlinked directory or file already under `baseDir` could redirect an otherwise safe key outside the cache root.
**Severity:** high
**Repro test:** TestCache_Path_PathTraversalSymlink_Bad
**Fix:** reject existing symlink components from the cache root through the resolved cache path before returning paths for filesystem use.

## 3. Eviction / TOCTOU
**Question:** Is `Cache.mu` (RWMutex) discipline correct? Does `Invalidate` walk the `invalidation` map race-cleanly while `OnInvalidate` may register more? On TTL expiry, do concurrent readers race?
**Finding:** TBD
**Severity:** TBD
**Repro test:** TBD
**Fix:** TBD
