# Task Breakdown

## Overview

Eliminate the per-open revalidate round-trip on the Mini App by serving the two
heavy static assets — `app.wasm` (~4.4 MB raw, ~1.2 MB pre-gzipped) and
`wasm_exec.js` (~17 KB) — under **content-hashed URLs** that nginx can cache at
the edge with `max-age=604800, immutable` (7 days). The hash changes on every
`make build` that alters the bytes, so deploys invalidate by *URL*, not by
header expiry — exact-version invalidation with no stale-content window.

Two HTML entry points (`cmd/web/static/index.html`,
`cmd/web/static/admin/index.html`) reference these assets via inline
`<script src="/wasm_exec.js">` and `fetch('/app.wasm')` calls. They are
rewritten in memory at boot (`bytes.ReplaceAll`) and served from a per-route
buffer cache. Unhashed `/app.wasm` and `/wasm_exec.js` URLs continue to resolve
via the existing `FileServer` fallback so a stale tab holding old HTML keeps
working — exactly the stale-HTML recovery semantics described in
`neuro_compass/plans/032-spa-js-hashing-and-nginx-cache-edge.md`.

Out of scope:
- HTML hashing (the SPA roots stay no-cache so a deploy is visible within
  one navigation).
- CSS / image / font hashing (no standalone CSS or image assets exist; all
  styling and JS is inlined into the two HTML files).
- CDN introduction.
- Build-time filename rewriting (runtime in-memory rewrite delivers the
  same invalidation semantics for this single-hop nginx → Go origin
  deployment — see neuro_compass plan, Trade-offs).

## Assumptions

- Single-hop deployment (`nginx → Go origin`). No CDN in front. Path-based
  cache keys chosen over query-string for future CDN-portability.
- Runtime in-memory HTML rewrite is acceptable (~2 ms of CPU at boot, zero
  ongoing infra cost) over a build-time rewrite (which would require a
  Makefile change and a dir restructure).
- The gzip snippet `configs/nginx.kz_behappy_gzip.conf` (committed at
  `e7a805c`) is already in place; this plan does NOT touch it. `wasm_exec.js`
  is `application/javascript`, which is already in `gzip_types`, so nginx
  will gzip it dynamically on the wire. No pre-gz sibling is shipped for it.
- `application/wasm` is **intentionally not** in `gzip_types` — the existing
  `wasmGzipHandler` at `cmd/web/main.go:204` serves the pre-built
  `app.wasm.gz` sibling itself. The hashed handler extends, not replaces,
  this behaviour.
- Stale-HTML recovery is guaranteed by the unhashed `FileServer` fallback:
  if a tab held HTML from a previous deploy, a request to `/app.wasm` (no
  hash) still resolves to the current bytes. The nginx hashed-asset regex
  does **not** match unhashed paths, so the fallback is cacheable only by
  the browser's default heuristics (i.e. effectively re-fetched on each
  reload) — that is exactly what we want for a recovery path.
- The user has accepted (per neuro_compass plan, Risks) that 8 hex chars of
  SHA-256 prefix are sufficient. 32 bits gives birthday-collision threshold
  near 65 k different versions of *the same asset*; two files across the
  project lifetime is nowhere near that ceiling.
- Both `be-happy.kz` (prime) and `stage.be-happy.kz` (stage) vhosts share
  `nginx.kz_behappy_common_settings.conf`. Edge cache rules added there
  apply to both environments simultaneously (intentional — stage must
  reproduce prime cache behaviour for verification).

## Tasks

### Task 1: Hash registry + hash-aware wasm/js handler in Go
- **Description:** Introduce a small `hashedAssets` registry that is built at
  boot from the embedded `static/` FS and consulted by the handler currently
  installed on `mux.Handle("/", ...)` at `cmd/web/main.go:212`. Each entry
  records: source path inside the embedded FS (`app.wasm`,
  `wasm_exec.js`), 8-hex SHA-256 prefix of the raw bytes, generated public
  URL (`/app.<hash>.wasm`, `/wasm_exec.<hash>.js`), content type, and the
  optional pre-compressed sibling path (only `app.wasm.gz` today).

  Place the new code at `cmd/web/hashedassets.go` (single-consumer rule:
  this struct is only used by `cmd/web/main.go`, per the placement memory).
  Expose a constructor `newHashedAssetRegistry(fsys fs.FS, specs []assetSpec) (*hashedAssetRegistry, error)`
  that fails fast (`return nil, fmt.Errorf("hashed asset: open %s: %w", ...)`)
  when a declared spec is missing — booting `cmd/web` without `app.wasm` is
  unrecoverable and `log.Fatalf` upstream is the correct response.

  Refactor the inline closure at `cmd/web/main.go:212-241` into a named
  function (e.g. `staticHandler(fsys http.FileSystem, embeddedSub fs.FS, registry *hashedAssetRegistry) http.Handler`)
  that, on each request, dispatches in this order:
  1. If `r.URL.Path` matches a registered hashed URL → serve from the
     registry. For `*.wasm` paths, apply the existing
     gzip-sibling-handoff logic (existing `httpenc.AcceptsGzip` +
     `Content-Encoding: gzip` + `Vary: Accept-Encoding`); for
     `*.js` paths, serve the raw bytes with
     `Content-Type: text/javascript; charset=utf-8` and let nginx handle
     gzip on the wire.
  2. Else if `r.URL.Path == "/"` or `"/index.html"` → serve the rewritten
     `index.html` from in-memory cache (only on `GET`/`HEAD`; other methods
     fall through to the FileServer; see Pitfalls below for the rationale).
  3. Else if `r.URL.Path == "/admin/"` or `"/admin/index.html"` → serve
     the rewritten `admin/index.html` from in-memory cache (same method
     guard).
  4. Else (unhashed `/app.wasm`, unhashed `/wasm_exec.js`, every API path
     that nominally proxies through `mux`, etc.) → fall through to the
     existing `fileHandler.ServeHTTP` for static files; the rest of the mux
     already has its own routes registered before `/`.

  Hash algorithm: SHA-256 over the raw (uncompressed) bytes, taking the
  first 8 hex characters (`fmt.Sprintf("%x", sha256.Sum256(b)[:4])`). This
  matches neuro_compass conventions; consistency across our projects beats a
  marginal width difference for two assets.

  The registry MUST be built from raw bytes, not from any precomputed
  hash file. Building from `app.wasm.gz` is wrong: the hash must reflect
  what the browser will execute (raw wasm), so a future change in `gzip`
  level alone does not invalidate the cache entry unnecessarily.

- **Acceptance Criteria:**
  - [ ] `cmd/web/hashedassets.go` exists with a `hashedAssetRegistry`
        struct, an `assetSpec` describing source path + content type +
        optional gzip sibling, and a constructor that returns
        `(*hashedAssetRegistry, error)`.
  - [ ] The constructor fails when `app.wasm` is missing from the
        embedded FS; a unit test asserts the error message names the
        missing path.
  - [ ] Both `/app.<hash>.wasm` and `/wasm_exec.<hash>.js` are reachable
        and return byte-identical content to the embedded source
        (assert via `bytes.Equal(got, embedded)`).
  - [ ] Hashed `*.wasm` URL returns `Content-Encoding: gzip` and
        `Vary: Accept-Encoding` when `Accept-Encoding: gzip` is set;
        returns plain bytes otherwise. `Content-Type` is
        `application/wasm` in both cases.
  - [ ] Hashed `*.js` URL returns
        `Content-Type: text/javascript; charset=utf-8`. (nginx gzip is
        applied on the wire; the handler emits raw bytes.)
  - [ ] Hash function is deterministic — two calls on the same input
        return the same 8 hex chars; a one-byte change in input produces
        a different prefix (subtest assertion).
  - [ ] Unhashed `/app.wasm` and `/wasm_exec.js` still resolve via the
        `fileHandler` fallback and return the same bytes — verified by
        a subtest that issues both URL forms against the same handler.
  - [ ] The boot log line `static directory: embedded FS` is followed
        by a log line of the form `hashed assets: app=<hash>
        wasm_exec=<hash>` so operators can sanity-check the active
        hashes after a deploy.
- **Pitfalls:**
  - Do not call `fs.ReadFile` on `app.wasm.gz` for hashing — hash the raw
    asset only. Otherwise a `make build` that changes only the gzip
    compression level (e.g. a Go toolchain bump) would invalidate the
    cache without changing the runtime behaviour.
  - The existing closure at `cmd/web/main.go:212` reads `embeddedSub`
    via `nil` check to gate the gzip handoff for the `--static-dir`
    override path. Preserve that: when `StaticDir != ""`, the registry
    is built from `os.DirFS(StaticDir)` instead of the embedded FS, and
    the gzip handoff still works. Without this, local dev with
    `--static-dir` breaks the wasm path.
  - Hashed-URL parsing must be exact, not prefix-based. A request for
    `/app.deadbeef.wasm.map` (sourcemap-style) must NOT match
    `/app.<hash>.wasm`. Use a map lookup keyed on the exact URL string,
    not a regex (we don't need one — there are two known paths).
  - The handler is installed at `mux.Handle("/", ...)`. All `/api/*`
    routes are already registered on `mux` before this catch-all, so the
    fallthrough order is correct: registered API routes shadow the
    catch-all, and the catch-all handles everything else.
  - `http.ServeContent` (already used in the existing `wasmGzipHandler`)
    expects an `io.ReadSeeker`. `embed.FS` files satisfy it but
    `os.DirFS`-opened files may not on every platform — guard with the
    existing `rs, ok := f.(io.ReadSeeker)` check and return a 500 if it
    fails (same defensive pattern as the existing code).
- **Complexity:** Medium.
- **Code Example:**
  ```go
  // assetSpec describes a hashable static asset and its optional precompressed sibling.
  type assetSpec struct {
      sourcePath  string // path inside fsys, e.g. "app.wasm"
      contentType string // "application/wasm"
      gzipPath    string // optional sibling inside fsys, e.g. "app.wasm.gz"; "" if none
  }

  // hashedAssetEntry is the resolved form of an assetSpec, ready for serving.
  type hashedAssetEntry struct {
      sourcePath  string
      hashedURL   string // "/app.<8hex>.wasm"
      contentType string
      gzipPath    string
  }

  // hashedAssetRegistry maps hashed URLs back to their source-file metadata.
  // Construction reads each declared asset, hashes its raw bytes, and registers
  // an in-memory entry; missing assets are a fatal startup error.
  type hashedAssetRegistry struct {
      byURL map[string]hashedAssetEntry
  }

  // newHashedAssetRegistry returns a registry built from specs against fsys.
  // It returns an error if any spec's source file cannot be read.
  func newHashedAssetRegistry(fsys fs.FS, specs []assetSpec) (*hashedAssetRegistry, error) {
      r := &hashedAssetRegistry{byURL: make(map[string]hashedAssetEntry, len(specs))}
      for _, s := range specs {
          b, err := fs.ReadFile(fsys, s.sourcePath)
          if err != nil {
              return nil, fmt.Errorf("hashed asset: read %s: %w", s.sourcePath, err)
          }
          sum := sha256.Sum256(b)
          prefix := hex.EncodeToString(sum[:4]) // 8 hex chars
          hashedURL := insertHash(s.sourcePath, prefix) // "app.wasm" -> "/app.<hash>.wasm"
          r.byURL[hashedURL] = hashedAssetEntry{
              sourcePath:  s.sourcePath,
              hashedURL:   hashedURL,
              contentType: s.contentType,
              gzipPath:    s.gzipPath,
          }
      }
      return r, nil
  }
  ```

### Task 2: Boot-time HTML rewrite for both entry points
- **Description:** At boot, after `newHashedAssetRegistry` succeeds, read
  `index.html` and `admin/index.html` from the same `fs.FS`, run two
  `bytes.ReplaceAll` passes per file (one for `/app.wasm`, one for
  `/wasm_exec.js`) substituting the hashed URLs, and cache the rewritten
  bodies in memory along with their last-modified time (use the embedded
  FS mtime, falling back to process boot time).

  Implement a tiny `htmlCache` (also in `cmd/web/hashedassets.go`) with one
  method `serve(w http.ResponseWriter, r *http.Request, path string)` that:
  - returns false (not handled) on any method other than `GET` or `HEAD`,
  - sets `Content-Type: text/html; charset=utf-8`,
  - uses `http.ServeContent` with a `bytes.NewReader` so range requests and
    `If-Modified-Since` work naturally.

  Wire it into `staticHandler` such that requests for `/`, `/index.html`,
  `/admin/`, and `/admin/index.html` are served from cache. Note the
  trailing-slash equivalence: nginx will issue `/` and `/admin/`; a direct
  `curl` may target `/index.html` or `/admin/index.html`. All four routes
  must yield the rewritten HTML.

- **Acceptance Criteria:**
  - [ ] After boot, `GET /` returns HTML with no occurrence of the
        literal strings `"/app.wasm"` or `"/wasm_exec.js"` (only their
        hashed forms appear).
  - [ ] Same for `GET /admin/`.
  - [ ] `GET /index.html` and `GET /admin/index.html` produce
        byte-identical bodies to `GET /` and `GET /admin/` respectively
        (the FileServer's existing `/index.html` → `/` 301 redirect no
        longer fires because the handler intercepts both).
  - [ ] `POST /` falls through to the FileServer (verified by a subtest
        asserting status 405 or whatever the FileServer returns — the
        point is that the rewritten HTML is NOT served on non-idempotent
        methods).
  - [ ] On a content change to `app.wasm`, restarting `cmd/web` produces
        HTML referencing a new `/app.<hash>.wasm` URL (subtest with two
        registry constructions over differing bytes).
  - [ ] Cached bodies are computed once at boot, not on every request
        (asserted by snapshotting the rewritten body pointer and
        confirming it does not change across requests).
- **Pitfalls:**
  - The two HTML files contain other occurrences of the literal string
    `app.wasm` (e.g. line 116 of `index.html` mentions "baked into
    app.wasm at build time" in a comment). `bytes.ReplaceAll` of the
    bare token `app.wasm` would corrupt prose. Replace only the exact
    URL forms: `"/app.wasm"` (with leading slash and in quotes is too
    brittle — quotes vary between single and double in the two files).
    The safest replacement targets are `/app.wasm` and `/wasm_exec.js`
    (with leading slash, no quotes). Verify by grepping both files for
    these two literals and confirming every match is inside an
    HTML attribute or a `fetch()` call, not in prose.
  - The unhashed-FileServer fallback at `/app.wasm` and `/wasm_exec.js`
    must remain reachable for stale-HTML recovery. The
    rewrite-and-cache step is for the served HTML only; the underlying
    embedded FS is untouched. Do not skip the FileServer registration.
  - `http.ServeContent` writes a `Last-Modified` header from the
    provided `time.Time`. Use a stable boot-time `time.Now()` so two
    pods serving the same build produce the same value (otherwise a
    load-balanced revalidate could flap). On single-host fx_rate_monitor
    this is academic; doing it right costs nothing.
  - When the user runs `cmd/web --static-dir ./cmd/web/static`, the
    HTML cache must read from disk through the same `fs.FS` passed to
    the registry, NOT a stale embedded copy. Build the cache lazily
    from whichever FS is active.
- **Complexity:** Medium.

### Task 3: nginx edge cache rules for hashed asset paths
- **Description:** Add a regex `location` block to
  `configs/nginx.kz_behappy_common_settings.conf`, **before** the
  catch-all `location /`, matching the two hashed-asset URL families:
  ```
  location ~* "^/(app|wasm_exec)\.[a-f0-9]{8}\.(wasm|js)$" {
      proxy_pass http://$backend$request_uri;
      proxy_hide_header Cache-Control;
      expires 7d;
      add_header Cache-Control "public, max-age=604800, immutable" always;
  }
  ```
  Read the neuro_compass `proxy_set_header` inheritance discussion before
  finalising. The decision for fx_rate_monitor: **repeat the four
  `proxy_set_header` directives inside the new regex location**, not hoist
  them to server scope. Justification:
  - `nginx.kz_behappy_common_settings.conf` is included from inside two
    `server {}` blocks in `nginx.kz_behappy.conf`. Server-scope hoisting
    would require either splitting the common-settings file (changing the
    deploy footprint of an unrelated file) or duplicating the directives
    in each server block (defeating the point of the common-settings
    snippet).
  - Repeating four `proxy_set_header` lines inside one new location is
    local, mechanical, and immune to surprise interactions with the
    existing `/` block's error_page handler.
  - The neuro_compass plan hoisted because both vhosts there had
    pre-existing per-location proxy headers, making hoisting strictly
    cheaper. Here the proxy headers live in exactly one place (the `/`
    block in common-settings); repeating four lines in one new
    location is the cheaper, more obvious diff.

  Final location block:
  ```
  location ~* "^/(app|wasm_exec)\.[a-f0-9]{8}\.(wasm|js)$" {
      proxy_set_header Host $host;
      proxy_set_header X-Real-IP $remote_addr;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_set_header X-Forwarded-Proto $scheme;
      proxy_pass http://$backend$request_uri;

      proxy_hide_header Cache-Control;
      expires 7d;
      add_header Cache-Control "public, max-age=604800, immutable" always;
  }
  ```

  Place this block **before** `location /` in the snippet (regex
  locations are evaluated in source order; if the catch-all is hit
  first, the regex never matches).

- **Acceptance Criteria:**
  - [ ] `configs/nginx.kz_behappy_common_settings.conf` contains the new
        regex location block placed before `location /`.
  - [ ] `nginx -t` against the local config (or the staging host)
        passes after the change.
  - [ ] `curl -I https://stage.be-happy.kz/app.<hash>.wasm` returns
        `Cache-Control: public, max-age=604800, immutable` and no other
        `Cache-Control` value.
  - [ ] `curl -I https://stage.be-happy.kz/` returns NO long-cache
        header (it inherits whatever Go emits, currently nothing, which
        gives the browser's default "revalidate on reload" — exactly
        what we want for SPA roots).
  - [ ] `curl -I https://stage.be-happy.kz/api/stats` returns NO
        `Cache-Control: max-age=604800` (the regex only matches the two
        named asset families).
  - [ ] `curl -H "Accept-Encoding: gzip" -I
        https://stage.be-happy.kz/app.<hash>.wasm` returns
        `Content-Encoding: gzip` and `Vary: Accept-Encoding` — the Go
        upstream's pre-gz handoff still works through the new regex
        location.
- **Pitfalls:**
  - nginx evaluates `location` blocks in source order for regex; the
    new block MUST be ABOVE `location /` in the file, or it never
    matches (the catch-all `/` swallows the request first because
    prefix `location` precedes regex evaluation only when the prefix
    match is *not* `^~`-prefixed — but the catch-all `/` is a
    universal prefix match and is evaluated as a candidate; the regex
    block must therefore come first physically in the include).
    Read the existing snippet carefully: the `location /` block ends
    with a nested `location = /custom_50x.html` for error pages — do
    not insert the new block *inside* that nested location.
  - `proxy_hide_header Cache-Control` removes any `Cache-Control`
    header the Go upstream sends. The Go upstream currently emits none
    for these paths (no static-cache middleware exists), so this is a
    belt-and-braces guard against a future regression where a
    middleware accidentally tags everything.
  - `add_header ... always` is required to set the header on
    non-2xx/3xx responses too (e.g. a 304 Not Modified from
    `http.ServeContent` should still carry the cache directive,
    otherwise the browser revalidates the cached entry needlessly).
  - The regex is intentionally tight: exactly `[a-f0-9]{8}` and a
    fixed extension. Loose regexes like `\.[^/]+\.(wasm|js)` would
    cache unhashed paths and break stale-HTML recovery.
  - Be explicit that gzip negotiation still happens at the Go layer
    for `.wasm` (it sets `Content-Encoding: gzip` itself); nginx's
    gzip snippet excludes `application/wasm` from `gzip_types` on
    purpose.
- **Complexity:** Easy.

### Task 4: Test coverage
- **Description:** Add `cmd/web/hashedassets_test.go` covering the new
  registry and HTML cache. Follow the project convention: one top-level
  `Test<Symbol>` per tested method/function, scenarios as `t.Run`
  subtests. Mocks (none expected here, since `fs.FS` is satisfied by
  `fstest.MapFS`) carry compile-time `var _ FS = &mockFS{}` assertions
  if introduced.

  Test functions:
  - `TestNewHashedAssetRegistry` — happy path, missing-file error, two
    hash determinism subtests (same bytes → same hash; differing bytes
    → different hash), URL shape (`/app.<8hex>.wasm`,
    `/wasm_exec.<8hex>.js`).
  - `TestHashedAssetRegistry_Serve` (or the equivalent method name) —
    serves hashed `*.wasm` plain when `Accept-Encoding` is absent,
    serves gz when `Accept-Encoding: gzip` and a sibling exists,
    serves plain when sibling is missing, serves `*.js` with
    correct content type. Verify bytes match the source file in each
    case.
  - `TestHTMLCacheRewrite` — both `index.html` and `admin/index.html`
    are rewritten correctly: no occurrence of `"/app.wasm"` or
    `"/wasm_exec.js"` remains in the *attribute/fetch* positions, while
    prose mentions like `"baked into app.wasm at build time"` are
    preserved. (This is a literal byte test; if it fails, the
    rewrite is targeting the wrong substring.)
  - `TestStaticHandler` — integration-style: build a tiny `fstest.MapFS`
    with stub `app.wasm`, `app.wasm.gz`, `wasm_exec.js`, `index.html`,
    and `admin/index.html`, install the handler, exercise eight URLs:
    - `/`, `/index.html` → rewritten HTML, GET only.
    - `/admin/`, `/admin/index.html` → rewritten admin HTML, GET only.
    - `/app.<hash>.wasm` → asserts gz vs plain depending on header.
    - `/wasm_exec.<hash>.js` → asserts JS content type + body.
    - `/app.wasm` (unhashed) → falls through to FileServer, returns
      raw bytes (stale-HTML recovery path).
    - `POST /` → not served from cache (asserts method guard).
  - `TestNewHashedAssetRegistry_MissingAsset` (subtest under the
    top-level constructor test) — declares an `assetSpec` whose
    `sourcePath` is absent from the FS, asserts the returned error
    wraps the path string.

  Use `t.Parallel()` on subtests where there is no shared mutable state.

- **Acceptance Criteria:**
  - [ ] `make test` passes with the new file present.
  - [ ] Coverage of `cmd/web/hashedassets.go` is ≥ 85 % (informational
        target; not a hard gate, but a signal that the test surface
        matches the public method count).
  - [ ] No `_ = err` discards in the new test file; every error path
        is asserted explicitly (per project convention).
  - [ ] `fstest.MapFS` is used in lieu of touching the real embedded
        FS — keeps the test hermetic.
- **Pitfalls:**
  - The hash is over raw bytes; the test must use a known-content stub
    (e.g. `[]byte("WASM_STUB")`) and compute the expected 8-hex prefix
    inside the test (`sha256.Sum256([]byte("WASM_STUB"))[:4]`) instead
    of hard-coding it. Hard-coded prefixes rot the test when the stub
    bytes change.
  - The HTML rewrite test must guard against false negatives: a test
    that only checks `bytes.Contains(rewritten, hashedURL)` would pass
    even if the original `/app.wasm` is left in the file. Assert both
    the presence of the hashed form AND the absence of the unhashed
    form **at known positions** (e.g. inside the `<script src=...>`
    tag).
  - For the `POST /` subtest, do not assume a specific status code —
    just assert the body does NOT contain the hashed URL (the
    FileServer either redirects, returns 405, or 200 depending on Go
    version; the behaviour we care about is "rewritten HTML is not
    served").
- **Complexity:** Medium.

### Task 5: Documentation update in CLAUDE.md
- **Description:** Add a short "Static asset caching" subsection at the
  end of the "HTTP Routes" section in `CLAUDE.md`, after the table of
  endpoints and the `/admin/` line. Cover:
  - The two hashable assets and their hashed URL shapes.
  - The boot-time HTML rewrite mechanism.
  - The nginx regex location and its 7-day `Cache-Control: immutable`
    directive.
  - The stale-HTML recovery path via unhashed `FileServer` fallback.
  - A note that `wasm_exec.js` is dynamically gzipped at the nginx
    layer (because `application/javascript` is in `gzip_types`), while
    `app.wasm` continues to use the pre-built `.gz` sibling handed off
    by the Go origin.

  Do NOT touch `nginx.kz_behappy_gzip.conf` documentation — gzip is
  already documented in that file's header comment.

- **Acceptance Criteria:**
  - [ ] `CLAUDE.md` contains a new "Static asset caching" subsection
        under "HTTP Routes".
  - [ ] `grep -n "app\.<hash>\.wasm" CLAUDE.md` finds the new doc.
  - [ ] No emojis, no marketing fluff (per project rules).
- **Pitfalls:**
  - `CLAUDE.md` is the contract; memory is the cache. Per memory entry
    `feedback_persist_project_policies_to_claude_md.md`, substantive
    policy must land in `CLAUDE.md`, not only in agent memory.
- **Complexity:** Easy.

## Execution Order

Strictly:
1. **Task 1** (hash registry + handler) — required by everything else; new
   file, mostly green-field.
2. **Task 2** (HTML rewrite) — depends on Task 1's registry exposing
   `(sourcePath, hashedURL)` pairs.
3. **Task 4** (tests) — implementer can write tests incrementally
   alongside Tasks 1 and 2; finalise once both are wired.
4. **Task 3** (nginx config) — independent of Go changes, but must
   reference the URL shape decided in Task 1. Can be done in parallel
   with Tasks 1–2 once the regex is agreed.
5. **Task 5** (docs) — last, because the documentation references the
   final URL shape, the nginx block, and the test surface.

Tasks 3 and 5 can be parallelised with the Go work; Tasks 1, 2, 4 must
proceed in order within the Go file because the test surface in Task 4
asserts the contracts defined in Tasks 1 and 2.

After all tasks pass `make test` and a local `cmd/web` boot prints the
expected `hashed assets:` log line, the engineer hands off to the
reviewer fan-out. Deploy + edge verification (running the `curl -I`
acceptance checks against `stage.be-happy.kz`) is the next-session
boundary, not part of this plan.

## Risks

- **Stale-HTML referencing dead hash.** Mitigated by the unhashed
  `FileServer` fallback (Task 1 step 4) and by SPA roots `/` and
  `/admin/` carrying no long-cache header. The worst case is a
  user-agent with a stale HTML reference for ~minutes after a deploy;
  it reloads `/app.wasm` (no hash) and gets the current bytes.
- **Hash collision (8 hex / 32 bits).** Birthday-collision threshold
  ≈ 65 k versions of the *same asset*. We have two assets and
  ~weekly deploys at the high end; the threshold is unreachable
  within the project's lifetime. If it ever becomes a concern, widen
  the slice to 6 bytes (12 hex chars) in `newHashedAssetRegistry`;
  the nginx regex must be widened in lockstep (`[a-f0-9]{12}`).
- **nginx regex placement regression.** A future edit that moves
  `location /` above the new regex block would silently break the
  edge cache. Mitigated by an inline comment in the snippet
  explaining the ordering requirement. Reviewers must flag any
  reorder.
- **Backend-set `Cache-Control` bypass.** `proxy_hide_header
  Cache-Control` inside the new regex location strips any
  `Cache-Control` the Go origin emits before the `add_header` runs.
  This is intentional: the regex location IS the source of truth for
  the hashed-asset cache policy. If the Go origin ever needs to emit
  `no-store` on these paths, this regex must change first.
- **`--static-dir` dev override.** Building the registry from
  `os.DirFS(StaticDir)` is required so local dev exercises the same
  code path as the embedded-FS production build. A subtle bug here
  (e.g. forgetting to switch FS roots) would surface only in dev, not
  in CI. Mitigated by Task 4's hermetic `fstest.MapFS` test, which
  uses the same constructor path as production.
- **HTML rewrite false matches in prose.** Line 116 of
  `index.html` mentions `app.wasm` in a comment. Replacing the bare
  token would corrupt prose. Task 2 specifies replacing only the
  exact attribute/fetch forms `/app.wasm` and `/wasm_exec.js` to
  avoid this; reviewers must verify the replacement string is
  precise.

## Trade-offs

- **Runtime in-memory rewrite vs build-time filename rewrite.** Chose
  runtime. Cost: ~2 ms of CPU at boot; benefit: zero Makefile or dir
  restructure. The neuro_compass plan made the same call for the
  same reasons. fx_rate_monitor's build pipeline is simpler than
  neuro_compass's, so build-time rewriting would be even more
  proportionally invasive.
- **Hash 2 assets vs the whole static graph.** Chose 2. There are no
  standalone CSS, image, or font assets — everything except wasm and
  wasm_exec.js is inlined into the HTML. Expanding the registry is a
  one-liner per new asset if that ever changes.
- **Path-based hash vs query-string version.** Chose path-based
  (`/app.<hash>.wasm`). Marginally more Go code (~20 LOC of URL
  parsing) than `?v=<hash>`; robust to any future CDN drop-in that
  ignores query strings on cache keys. Same call as neuro_compass.
- **`max-age=604800` (7 d) vs `max-age=86400` (24 h) vs `max-age=31536000` (1 y).**
  Chose 7 d. Hashing provides perfect URL-level invalidation (a new
  build produces a new hash, so the rewritten HTML points users at a
  fresh URL on the next navigation regardless of the prior URL's
  cache TTL), which makes the cache-duration choice a question of
  amortisation, not safety. 24 h was the initial conservative pick
  but forced a revalidate round-trip on every user who skipped the
  app for a day — losing the win on the exact engagement pattern this
  feature was meant to help. 1 y is industry-standard but adds
  nothing practical on a single-host, single-origin deployment with
  weekly-ish build cadence: a returning user who has not visited for
  a full year is also returning to a fresh HTML that re-issues
  whatever URL is current. 7 d covers the typical "open the Mini App
  several times a week" engagement window and still leaves a
  forensic backstop in the unlikely event of needing to wait out a
  specific cache entry.
- **Regex `location` vs `map` directive.** Chose regex `location`.
  `map` is `http`-context-only and would require either a separate
  config file or a per-environment variable name; for two hashed
  asset families the regex is shorter and obvious. Future-proof: a
  third asset is a one-character regex tweak.
- **Repeat `proxy_set_header` directives vs hoist to server scope.**
  Chose to **repeat** the four directives inside the new regex
  location. The common-settings snippet is included from two
  `server {}` blocks; server-scope hoisting would require either
  splitting the snippet or duplicating directives per-vhost.
  Repeating four lines in one new location is the cheaper, more
  obvious diff and matches the spirit of nginx's all-or-nothing
  inheritance rule (see neuro_compass plan, Task 2, "Pitfalls").
- **Pre-build `wasm_exec.js.gz` sibling vs nginx dynamic gzip.** Chose
  nginx dynamic. `application/javascript` is in `gzip_types`, the
  file is 17 KB (gzip is microseconds), and shipping a pre-gz sibling
  would mean teaching the Go handler one more conditional. The cost
  is one CPU-millisecond per cache miss at the edge — negligible.
- **HTML cache mtime: boot time vs file mtime.** Chose boot time. The
  embedded FS reports per-file mtime as the `go build` invocation
  timestamp, which is identical to boot time on a fresh deploy. On a
  hypothetical `--static-dir` dev override, using boot time means
  edits to HTML during a single `cmd/web` run are NOT picked up
  until restart — acceptable, because dev iteration uses Go rebuilds
  anyway.
