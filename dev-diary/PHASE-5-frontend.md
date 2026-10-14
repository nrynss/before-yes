# P5: Frontend (Track D)

```yaml
id:       P5
size:     M
requires: [T1.3, T1.4]
blocks:   P6
parallel: high
runs-parallel-with: P2, P3, P4
```

**Goal:** One page a parent can act on: type a title and an age, watch the check read the sources, land on a card. The card shows the verdict, every red flag linked to its quote, the settings to turn on, and safer alternatives.

**Builds offline:** Every view builds against the example payloads from T1.3 and a mock events stream, so this track never waits for P3 or P4.

**Stack:** Svelte 5 with runes on Vite. [Chaaya](https://github.com/nrynss/chaaya) 0.4.1 is a declared dependency: its Keel adapter supplies `JobStream` and `JobFollower` for progress, `api` with `keelErrorParser` and `apiWithKeelGate` for typed errors and the passcode, and its theme tokens with the contrast and accessibility gates. Bits UI supplies dialogs and tooltips. A gap in Chaaya becomes a Chaaya issue text in the handoff log, not a workaround.

**Video rule:** No third-party logos or branded artwork anywhere in the UI. A title's name appears only as text on a card. Do not pull app icons from the App Store.

---

### T5.1: App shell and theme
```yaml
requires:   T1.4
fixture-ok: yes
size:       S · mid
owns:       web/package.json, web/vite.config.ts, web/src/app.html, web/src/lib/theme/, web/src/routes/+layout.svelte, plus the shell's other config at web/ root (pnpm-lock.yaml, svelte.config.js, tsconfig.json, lint and test config) and a placeholder +page.svelte per route
status:     done
```
Scaffold the app with Chaaya's theme tokens. Light and dark themes, both passing Chaaya's contrast gate. Phone first: a parent asks on the spot, with the child standing there.

Routes: `/` (ask), `/check/{id}` (progress), `/card/{id}` (card), `/compare/{a}/{b}` (side by side).

**Done when:** The shell builds, both themes pass the contrast gate, and every route renders at 360 px wide with no horizontal scroll.

---

### T5.2: Ask ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/+page.svelte, web/src/lib/ask/, web/playwright.config.ts (ask test discovery)
status:     done
```
Two fields: what the child asked for, and the child's age (3 to 17). One button. Recent cards from `GET /api/cards` sit below as examples a judge can open without spending anything.

A `200` with a card id goes straight to the card. A `202` goes to the progress view. A passcode refusal opens a passcode dialog. `live_checks_off` points at the example cards.

**Done when:** Each answer from T1.4's examples routes correctly, and the form works by keyboard alone.

---

### T5.3: Progress ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/check/, web/src/lib/progress/, web/playwright.config.ts (module browser test discovery), web/package.json and web/pnpm-lock.yaml (Chaaya 0.4.2 adoption only), web/pnpm-workspace.yaml (exact-version Chaaya 0.4.2 release-age exception)
status:     done
```
Chaaya's `JobStream` follows `GET /api/checks/{id}/events` and catches up from `GET /api/checks/{id}` after a reconnect. Show the three stages, gather, read and judge, with the server's message ("reading 312 reviews…") and a count when the stage counts.

On done, go to the card. On failure, show the message for the specific code from `GET /api/checks/{id}`, never the generic `job_failed`.

**Done when:** Against a mock stream, the view walks the three stages, survives a dropped connection mid-read, and shows the right message for each failure code.

---

### T5.4: The card ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       L · frontier
owns:       web/src/routes/card/, web/src/lib/card/, web/playwright.config.ts and web/shell.spec.ts (card test discovery and fixture-backed shell smoke pins); ID-only updates to testdata/contracts/card-*.json, testdata/contracts/api/*, internal/pipeline/types_test.go, internal/pipeline/api_examples_test.go, internal/server/checks_test.go, web/src/lib/ask/ask.spec.ts
status:     done
```
The single page a parent acts on, in this order:
1. **Verdict**: yes, yes with these settings, or no, with the title, the age, and when it was checked.
2. **Settings to turn on**, when there are any, as a checklist the parent can follow.
3. **The six areas**, each with its grade and one plain sentence. A flagged area shows its quote inline, with the source: "review 312", "privacy policy", or a forum thread. The quote links to its source.
4. **Safer alternatives**, each with its verdict and a link to its own card and to the side-by-side. A card carries only checked alternatives. When the daily cap refused any, it carries `alternatives_note` ("More alternatives when today's checks reset") instead of their names; show that one line.
5. **Small print**: the kind (`kind_label`, for example "kind unknown"), sources read, reviews read, quotes dropped as unverifiable, the checklist version, the models used, and what the check cost.

The review 312 moment must read at a glance. The quote that decided the card is the most visible thing on it after the verdict.

Every card has a share link. It is the card's own URL.

**Done when:** Every example card from T1.3 renders, the "no" card leads with its deciding quote, and the card passes Chaaya's accessibility gate.

---

### T5.5: Side by side ★
```yaml
requires:   T5.4
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/compare/, web/src/lib/compare/
status:     not-started
```
Two cards, area by area, in one grid. The fixed checklist is what makes two titles comparable. This view shows that, and it closes the demo.

On a phone the grid becomes one row per area, holding both grades.

**Done when:** Comparing the "no" example card with its alternative renders correctly on desktop and at 360 px.

---

### T5.6: Errors and empty states
```yaml
requires:   T5.2, T5.3
fixture-ok: yes
size:       S · light
owns:       web/src/lib/errors/
status:     not-started
```
One message per code in T1.4: `budget_exhausted`, `no_sources`, `check_failed`, `live_checks_off`, `busy`, `rate_limited`, `passcode_required`, `not_found`. Each tells the parent what to do next. None shows a stack trace or a raw code.

A card with an `unknown` area explains that not enough evidence was found, rather than looking like a pass.

**Done when:** Each code has a test rendering its message, and every message has a next step.

---

### T5.7: Visitor keys dialog
```yaml
requires:   T5.2, T4.5
fixture-ok: yes
size:       S · mid
owns:       web/src/lib/keys/
status:     in-review
```
When the daily cap is spent, offer to run the check on the visitor's own Token Factory and Tavily keys. Say plainly that the keys are used for this one check and not stored. The keys live in component state only: never in local storage, the URL, or a log.

**Done when:** A check runs on pasted keys against a mock, and a test confirms nothing is written to storage or the URL.

**Note (T5.7).** `Keys.svelte` posts `{title, age}` to `/api/checks` with both `X-Token-Factory-Key` and `X-Tavily-Key`, or it sends nothing. A half pair, a space, a non-printable byte, or a key over 512 bytes is refused in fixed copy that does not repeat what was typed. `200` `card_id` and `202` `check_id` open only when the id is `c_` or `j_` plus 32 lowercase hex. The default client is Keel's `api`; a parent may pass another, including the passcode-gated one. Ask.svelte does not mount the dialog because it is outside owns. A later page mounts `Keys` with the title and age it already has.

---

## Exit Criteria

- [ ] Ask, progress and card views work end to end against the examples and a mock stream.
- [ ] The "no" card leads with its deciding quote.
- [ ] The side-by-side renders on desktop and phone.
- [ ] Both themes pass Chaaya's contrast and accessibility gates.
- [ ] No third-party logo or artwork anywhere in the UI.
