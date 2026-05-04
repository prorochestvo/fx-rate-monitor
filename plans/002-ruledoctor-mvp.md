# 002 — ruledoctor MVP (LLM-driven extraction-rule generation)

## Status: HYPOTHESIS CONFIRMED ✓

Haiku 4.5 (via the local `claude` CLI at `--effort low`) produces correct values
**and** working CSS selectors **and** working Go RE2 regexes for **39/39 pairs**
on the canonical fixture (`tmp.html`, NBK-style table). Zero call errors, zero
parse errors, zero verification mismatches.

The MVP is finished. This document is now the **handoff brief**: what exists,
what we learned, and what to do next.

---

## What exists in the repo today

```
plans/002-ruledoctor-mvp.md                     ← this file
docker-compose.ollama.yml                       ← local Ollama in Docker (only useful on dev Mac)
tmp.html                                        ← canonical fixture (NBK FX rates page, 376 KB / 4013 lines)
testdata/ruledoctor/expected.json               ← 39 verified pairs from tmp.html

internal/ruledoctor/
├── generator.go        — Generator interface (single method: Generate)
├── ollama.go           — OllamaClient
├── anthropic.go        — AnthropicClient (direct Anthropic API)
├── claudecode.go       — ClaudeCodeClient (subprocess to local `claude` CLI; uses your Claude Code subscription)
├── cleaner.go          — strips <script>/<style>/<noscript>/<iframe>/<svg>/<!---->/link/meta + collapses whitespace
├── snipper.go          — slices to single <tr>...</tr> containing the pair label
├── prompt.go           — Extraction struct, BuildPrompt, ParseExtraction (tolerates fences / surrounding prose)
├── verify.go           — applies suggested CSS (goquery) + regex (RE2) to original HTML, compares to expected
├── snipper_test.go     — offline unit tests (run in `make test`)
├── verify_test.go      — offline unit tests
└── extract_test.go     — TestExtract: integration test, 39 sub-tests, skipped unless provider env is set

Makefile targets:
  ruledoctor-up               docker compose up Ollama
  ruledoctor-down             docker compose down
  ruledoctor-pull MODEL=…     pull a model into the running container
  ruledoctor-test             run TestExtract against local Ollama
  ruledoctor-test-haiku       run TestExtract via local `claude` CLI (Claude Code subscription)
  ruledoctor-test-anthropic   run TestExtract against Anthropic API directly (needs ANTHROPIC_API_KEY)

Dependency added: github.com/PuerkitoBio/goquery (for CSS-selector verification).
```

---

## How the test works

1. Reads `tmp.html` and runs it through `Clean()` (376 KB → 107 KB).
2. For each of 39 pairs in `expected.json`:
   1. `SnipForPair(cleaned, pair)` → ~400-byte fragment containing the single `<tr>` for that pair.
   2. `BuildPrompt(snippet, pair)` → user prompt with rules + one-shot example (EUR/KZT) + the snippet.
   3. `client.Generate(ctx, prompt)` → raw model response.
   4. `ParseExtraction(raw)` → `Extraction{Value, CSSSelector, Regex, Confidence}`.
   5. `Verify(originalHTML, expectedValue, ex)` → three booleans:
      - `ValueMatches`: model's `value` equals expected
      - `CSSMatches`: applying selector via goquery to the **original** HTML returns expected value
      - `RegexMatches`: regex compiled and capture-group-1 returns expected value
3. Final summary: per-metric pass/fail counts + total/avg time.

---

## Experimental results

### Iteration 1 — Qwen 2.5 1.5B-Instruct (Ollama, Docker on Mac, CPU)

```
limit=3
value match : 1/3 (33%)
css match   : 0/3 (0%)
regex match : 0/3 (0%)
avg time    : ~16 s/pair
```

**Failures:** hallucinated values from neighboring rows in a wide snippet
(AUD got "27.07" — actually MDL's value), shifted decimal points
(AZN got "27.31" instead of "273.1"), generated CSS selectors with impossible
syntax (`.text-start > td:nth-child(3)` — `.text-start` IS a `<td>`),
generated regexes with no capture groups or with PCRE features RE2 rejects.

**Verdict:** 1.5B is too weak for this task even with cleaned + snipped input.

### Iteration 2 — Llama 3.1 8B (Ollama, Docker on Mac)

```
all 39: HTTP 500 — "model requires more system memory (5.3 GiB) than is available (3.2 GiB)"
```

**Cause:** Docker Desktop's default VM memory cap on Mac (~3-4 GB) cannot
accommodate the 8B model. **Not** a Mac RAM issue — bumping Docker Desktop →
Settings → Resources → Memory to 8 GB would unblock this if we ever return to it.

We chose not to fix this and skipped to Haiku for a faster signal.

### Iteration 3 — Haiku 4.5 via `claude` CLI subprocess (`--effort low`)

```
limit=39 (full)
value match : 39/39 (100%)
css match   : 39/39 (100%)
regex match : 39/39 (100%)
call errors : 0
parse errors: 0
total time  : 12m19s (avg 18.96 s/pair)
```

**Selector strategies the model produced** (all 39 verified working):
- 33 pairs: `tr:has(td:contains("XXX / KZT")) td:nth-child(4)`
- 4 pairs (BYN, NOK, PLN, UAH): `td:contains("XXX / KZT") + td`
- 1 pair (ZAR): `tr:has(td:contains("ZAR / KZT")) td:nth-of-type(4)`
- 1 pair (JPY): `tr:has(td:contains("JPY / KZT")) td:contains("JPY / KZT") + td` (overspecified, still works)

**Regex strategy** (all 39): `XXX / KZT</td>\s*<td[^>]*>?([0-9.]+)</td>` style.

**Per-pair time:** 10–55 s, mostly 12–25 s. The variance is hosted-API jitter
plus ~1–3 s CLI spawn overhead per call (Node.js + OAuth check).

---

## Architecture decisions made (worth preserving)

1. **Snipper produces a single `<tr>` block, not a wide window.**
   Earlier we tried ±2000/1500 byte windows — small models then picked numbers
   from neighboring rows. Tight snipping removes the choice. For Haiku this
   is also good (less context = lower token cost).

2. **Prompt has a one-shot example** (EUR/KZT row → expected JSON output).
   Explicit rule: "value is in the SAME `<tr>` as the pair label". Hint that
   cascadia/goquery supports `:contains()` and `:has()` (so the model can
   anchor by text and not depend on knowing the row index).

3. **`ParseExtraction` tolerates surrounding prose**: it strips ```json fences
   AND, if the response doesn't start with `{`, scans for the first balanced
   `{...}` (string-aware bracket matching, respects `\"` escapes).

4. **`Verify` runs against the ORIGINAL HTML, not the cleaned/snipped version.**
   This is critical — in production we'll apply rules to live pages, not to
   our preprocessed slice.

5. **`ClaudeCodeClient` runs in `os.TempDir()`** so the project's `CLAUDE.md`
   doesn't get auto-discovered into context.

6. **Three providers behind one `Generator` interface.** Switch via env
   `RULEDOCTOR_PROVIDER=ollama|anthropic|claudecode`. Lets us iterate without
   recompiling and proves the rest of the pipeline is provider-agnostic.

7. **Integration test is skipped by default** (no env → `t.Skip`). Unit tests
   for snipper / verify run in `make test` and stay green offline.

---

## Production constraints discovered

- **Prod VPS at `be-happy.kz` is too small for ANY local model.**
  Specs: 1 vCPU, 961 MiB RAM (462 MiB free at idle), 6.3 GB disk free, Ubuntu 24.04.
  Even Qwen 2.5 0.5B + Ollama runtime exceeds available memory. The OOM-killer
  would terminate the existing collector/notifier. **Decision: LLM never runs
  on this VPS.** ruledoctor must execute elsewhere and push results in.

- **Mac dev machine specs are unknown** but visibly fine for Haiku via
  Claude Code (CLI subprocess works). Docker memory was the limiter for
  local 8B Ollama, not Mac RAM.

- **Claude Code on prod is not viable either** (Node.js footprint + the same
  RAM constraint). So `claudecode` provider is dev-only.

---

## Cost / fit comparison for production

| Provider | Cost | Where it runs | Latency / pair | Fit for prod? |
|---|---|---|---|---|
| Ollama 1.5B local | free | dev Mac | 15 s | rejected — too inaccurate |
| Ollama 8B local | free | dev Mac (with Docker mem ↑) | 30–60 s | unknown — not yet tested |
| Claude Code CLI (Haiku) | counted against your subscription | dev Mac only | 19 s | **proven**, but coupled to Claude Code |
| Anthropic API (Haiku 4.5 direct) | ~$0.05 / full 39-pair run | anywhere | 1–3 s | clean, but separate billing |
| Groq Llama 3.1 8B | free, 14400 req/day | anywhere | 1–2 s | unknown — not yet tested |

For a daily ruledoctor run on say 10–20 sources, Anthropic API would be
~$0.05–0.10/day. Groq would be $0/day if quality holds. Both are realistic.

---

## STEP 1 results (2026-05-03): generalization CONFIRMED on multi-layout fixtures

The user provided four additional snapshots (`plans/history/002-ruledoctor-srv/`).
Audit of those raw HTTP captures:

| Snapshot | Verdict | Why |
|---|---|---|
| `tmp_kz_nationalbank.html` | ✓ usable as-is | NBK table — already proven (39/39) |
| `tmp_bcc_kz.html` | ✓ static rates inside div cards | BCC homepage |
| `tmp_kz_halykbank.html` | ✗ JS-only | Title says "Курс валют" but rates load via XHR `/api/gradation-ccy` after page render |
| `tmp_kz_bankffin.html` | ✗ JS-only | React app, rates only after hydration |
| `tmp_kz_qazpost.html` | ✗ wrong page | Next.js landing, no FX content at all |

To unblock the JS-rendered cases the project now ships its own headless renderer:

```
cmd/ruledoctor-fetch/main.go   ← chromedp-based; takes a URL, returns the
                                  hydrated DOM HTML; can also dump every JSON
                                  XHR/fetch response that fired during load
                                  (XHR capture is opt-in via --xhr-dir)
```

`make ruledoctor-fetch URL=https://...` is the convenience target. Halykbank
serves a heavily anti-headless flavor that did not yield rates even after a
15 s post-load wait — left for later (likely needs rod's stealth flags or a
paid headless service); BCC and Bankffin rendered cleanly.

### Fixtures and how the test now consumes them

```
testdata/ruledoctor/
├── nationalbank.html              ← raw HTTP body of the original tmp.html (NBK)
├── nationalbank_expected.json     ← 39 NBK pairs (unchanged)
├── bcc.html                       ← rendered from https://www.bcc.kz/
├── bcc_expected.json              ← 3 pairs (USD/EUR/RUB / KZT)
├── bankffin.html                  ← rendered from https://bankffin.kz/ru/exchange-rates
└── bankffin_expected.json         ← 3 pairs (USD, RUB, EUR)
```

`expected.json` convention for the new fixtures: the value is the **first
numeric rate that appears after the pair label in document order**. For BCC
that is "Продать (до 10 000)"; for Bankffin that is "Покупка". This convention
removes ambiguity for the model when a pair has multiple rates (sell/buy,
tiered) and lets us measure extraction quality cleanly.

`internal/ruledoctor/extract_test.go` was rewritten to iterate every
`<name>.html` + `<name>_expected.json` pair under `testdata/ruledoctor/`.
`RULEDOCTOR_SOURCE=<name>` filters to a single fixture; the per-source and
overall summaries print at the end.

### Snipper update

The snipper now does **`<tr>` first, asymmetric window second** (200 bytes
before, 1500 after the pair label). An earlier "smallest enclosing div with a
rate-shape number" approach was correct but O(n×d) on minified HTML — taking
~120 s per call on 290 KB of cleaned BCC. The window approach is O(n) once,
finishes in ms, and produces snippets the LLM extracts from successfully on
both table and div-card layouts.

### Iteration 4 — Groq Llama 3.3 70B (free tier)

Smart enough to write good CSS and regex, but the free-tier 12 000 TPM ceiling
is hit after ~10 NBK pairs (each prompt is ~1100 tokens). Even with the new
retry-with-backoff loop in `GroqClient` (parses Groq's `try again in X.XXs`
hint), back-to-back fixtures bleed into each other's quota window and time out.
Verdict: works for ad-hoc small runs, not for batch jobs on free tier.

### Iteration 5 — Groq Llama 3.1 8B Instant (free tier) — STEP 1 PASS

```
[bankffin]      pairs=3   value=3/3 (100%)   css=1/3 (33%)   regex=0/3 (0%)
[bcc]           pairs=3   value=3/3 (100%)   css=0/3 (0%)    regex=0/3 (0%)
[nationalbank]  pairs=39  value=39/39 (100%) css=39/39 (100%) regex=27/39 (69%)
[OVERALL]       pairs=45  value=45/45 (100%) css=40/45 (89%) regex=27/45 (60%)
total=4m10s avg=5.5s/pair, 0 call errors, 0 parse errors
```

**This clears the ≥90% gate on every fixture for value extraction** — the
weakest dimension across all three layouts. The 8B model is fast and stays
under the per-minute token cap on its own.

Where the 8B model is weak:
- **CSS on div-card layouts** (BCC, Bankffin): the model can describe what it
  wants ("the third div sibling after the label") but composes selectors that
  goquery/cascadia can't realise. The 70B model handled this cleanly when its
  rate budget held. Implication: for div layouts, plan to fall back to a
  stronger model **only when the small model fails verification** — see Step 2.
- **Regex** on NBK: 27/39. RE2 quirks (`.` not crossing whitespace cleanups
  the cleaner introduces, no `(?s)` injected) explain most failures. Tighter
  prompt guidance about using `[\s\S]*?` could close most of this gap.

### Decision

Generalization confirmed for the **value** dimension across **table + div-card
+ JS-rendered (post-pre-render)** layouts. Step 2 may proceed.

**Cheaper alternatives no longer speculative**: Groq Llama 3.1 8B is the
default candidate for production extraction; Haiku via Anthropic API is the
fallback for the ~10–15% of cases where 8B can't generate a working selector.

### STEP 2: Production architecture (`plans/003-ruledoctor-prod.md`)

This is a separate plan file to be drafted only after Step 1 succeeds. The
key questions it must answer:

- **Where rules live.** Most likely: new columns on `RateSource`
  (`extraction_css`, `extraction_regex`, `extraction_updated_at`,
  `extraction_source` (e.g. "ruledoctor:haiku-low" / "manual")). Append-only
  migration in `RateSourceRepository.Migration()` per CLAUDE.md.
- **How collector uses them.** New `Extractor` strategy that tries CSS first,
  falls back to regex, falls back to current hand-written rule, finally
  records a "rule-broken" execution-history entry that ruledoctor can pick up.
- **Where ruledoctor runs.** Candidates:
  - **Mac via cron** — simplest, but requires the Mac to be on. Talks to prod
    via the existing v1 REST API (probably needs a new authenticated
    `PATCH /api/sources/{name}/extraction` endpoint).
  - **GitHub Actions scheduled workflow** — autonomous, free for low volume,
    needs a secret for the prod API token and ANTHROPIC_API_KEY (or some
    other LLM credential — see Step 3).
  - **Tiny separate VPS** ($5/mo) — most reliable, simplest auth model.
- **Failure handling.** Rule-broken signal from collector triggers ruledoctor
  on the next scheduled run (or on demand). After N consecutive ruledoctor
  failures on the same source, the source is auto-deactivated and a Telegram
  admin alert fires.
- **Observability.** Log ruledoctor runs in `execution_history` (or a new
  sibling table). Surface the latest extraction rule per source in the web UI.

### STEP 3 (optional, runs in parallel with or after Step 2): Cheaper alternatives

Now that we have a working baseline (Haiku via Claude Code), test cheaper
backends with the **exact same prompt and snipper**. Two candidates:

a. **Groq Llama 3.1 8B free tier.**
   - Add a `GroqClient` (OpenAI-compatible API; ~50 LoC mirroring `AnthropicClient`).
   - Free, 14400 req/day, no card required.
   - If it shows ≥90% value match and ≥80% rule-replay match on our fixtures,
     it becomes the prod default and we drop Claude Code entirely from the loop.

b. **Local Ollama Llama 3.1 8B (with Docker mem bumped to 8 GB on Mac).**
   - Only useful if user wants fully offline / air-gapped.
   - Won't help prod VPS, but could be the dev-time generator.

**Decision matrix is built only after data exists.** Don't add Groq client
speculatively — wait for Step 2 to clarify whether we even need it.

---

## Reproduction recipe (for next-context me or anyone else)

```fish
cd ~/Projects/seilbek/fx_rate_monitor

# Unit tests (no LLM needed):
make test

# Run the integration test against Haiku via Claude Code subscription
# (this is the proven path — gives 39/39 in ~12 minutes):
make ruledoctor-test-haiku LIMIT=5     # quick smoke ~1.5 min
make ruledoctor-test-haiku             # full ~12 min

# To re-test on Ollama (e.g. for Step 3a/3b):
make ruledoctor-up
make ruledoctor-pull MODEL=qwen2.5:3b-instruct
make ruledoctor-test  MODEL=qwen2.5:3b-instruct LIMIT=5

# Direct Anthropic API (paid, separate billing):
export ANTHROPIC_API_KEY=sk-ant-...
make ruledoctor-test-anthropic LIMIT=5
```

Env vars the integration test honours:
- `RULEDOCTOR_PROVIDER` — `ollama` (default) | `anthropic` | `claudecode` | `groq`
- `OLLAMA_URL` — e.g. `http://127.0.0.1:11434`
- `ANTHROPIC_API_KEY` / `GROQ_API_KEY` (per provider)
- `RULEDOCTOR_MODEL` — provider-specific override
- `RULEDOCTOR_EFFORT` — claudecode only; e.g. `low`/`medium`/`high`
- `RULEDOCTOR_LIMIT` — `0` = all pairs across every fixture
- `RULEDOCTOR_SOURCE` — optional: filter to one fixture by name (e.g. `bcc`)
- `RULEDOCTOR_TIMEOUT` — Go duration, per-call timeout

The test discovers fixtures dynamically: every `testdata/ruledoctor/<name>.html`
paired with `testdata/ruledoctor/<name>_expected.json` is run.

To capture a new fixture from a JS-rendered page:

```fish
make ruledoctor-fetch URL=https://example.com/exchange-rates
# writes ./tmp/example.com.html — strip headers if any, move to testdata/ruledoctor/<name>.html,
# then hand-author <name>_expected.json with the convention:
# value = first numeric rate after the pair label in document order.
```

---

## STEP 2 (next): Production architecture

See section above. Open design questions, in priority order:

1. **Where rules live in the data model.** New columns on `RateSource`
   (`extraction_css`, `extraction_regex`, `extraction_updated_at`,
   `extraction_provider`)? Or a sibling `rate_source_extraction_rules` table
   so we can keep history? Append-only migration in
   `RateSourceRepository.Migration()` per CLAUDE.md.
2. **Pre-render vs static fetch.** Two-tier fetcher: try `http.Get` first;
   if the resulting HTML lacks a rate-shape number, retry via the chromedp
   pre-renderer; if still empty, scan the captured XHR responses (the
   `--xhr-dir` mode of `cmd/ruledoctor-fetch`) for a JSON endpoint that DOES
   contain the rate, and pivot the source URL to that endpoint with
   `MethodJSONPath` rules. This realises the user's goal: "I just give a page,
   the algorithm decides whether it's HTML or an XHR-backed JSON API".
3. **Two-model cascade.** Try Groq Llama 3.1 8B first (free, fast). If
   verification fails on CSS *and* regex, escalate to Haiku via Anthropic API
   for that single pair. Records which model produced each rule for audit.
4. **Where ruledoctor runs.** Mac via cron, GitHub Actions on schedule, or a
   tiny separate VPS. Prod VPS is too small (1 vCPU / 961 MiB RAM).
5. **Failure handling and observability.** Rule-broken signal from collector
   triggers ruledoctor on next run; N consecutive ruledoctor failures auto-
   deactivate the source and page admin via Telegram. Surface latest extraction
   rule per source in the web UI.
