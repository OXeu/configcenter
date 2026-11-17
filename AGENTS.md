# AGENTS.md

Rules for anyone (human or model) changing configcenter. They are enforced where a tool exists; where a rule is judgement, it is listed as such.

## Layout

- `cmd/configcenter` — main, flags-free, environment driven. `internal/config` loads `.env.local` (or `CC_ENV_FILE`) with godotenv before reading variables; exported variables are never overridden. Never print secret values.
- `internal/config` — loads the env file and reads every environment variable here and nowhere else.
- `internal/store` — gorm models and all queries; one `Store` method per query.
- `internal/auth` — sessions, roles, CSRF, API-key and agent middleware, OAuth provider adapter.
- `center-common/oauth` — the shared RFC 6749 + OIDC client (discovery, PKCE, id_token verification, userinfo fallback).
- `internal/server` — gin router, REST handlers, MCP endpoint, Go templates (`templates/shell/vue.html`), embedded static files.
- `web/ui` — Vue 3 single-file components and Tailwind CSS; Vite Plus builds into `.tmp/ui`, embedded by root `assets.go`.
- `sdk/configcenter` — separate module with the public Go client.
- `docs/commits.md` — the commit message rules (Conventional Commits 1.0.0 plus this repository's constraints).

## Commits

Commit messages follow [`docs/commits.md`](docs/commits.md): Conventional Commits 1.0.0 with the repository's own constraints. When asked to commit, split the work into one commit per functional change and keep committing until `git status` is clean — never commit only the part of the working tree you touched. The full procedure is in that document; do not restate or shorten it here.

## Files created while working

1. **Nothing but source and test code goes into the source tree.** Scratch scripts, debug programs, logs, traces, generated databases, fixtures, coverage output, build output and one-off experiments are not created under `cmd/`, `internal/`, `sdk/`, `web/`, `tools/` or `docs/`. Test code is a committed `*_test.go` / `web/ui/__tests__/*.test.ts` file; nothing else is added to a package directory, and a test that opens a database or a filesystem path opens it in memory or under `.tmp/`.
2. **Every artifact goes to `.tmp/` at the repository root.** Use `.tmp/<subdirectory>` for anything a command, test or experiment writes. `.tmp/` is gitignored and never committed.
3. **`/tmp` and friends are forbidden**, including the system temp dir, `t.TempDir()`, `os.MkdirTemp("", …)`, `os.CreateTemp("", …)`, `fs.mkdtempSync` outside `.tmp` and shell `mktemp`. A test that needs a directory gets one under `.tmp/`, from a path built on the repository root.
4. **Clean up.** Delete what you created under `.tmp/` when you are done; if it must stay for later inspection, say so and give its path.
5. **Go's scratch space.** `go test` and `go build` write their intermediate working directory to `$TMPDIR`, which defaults to `/tmp`; `go` removes it on exit. `make test` sets `TMPDIR` to `.tmp`, so `make test` and `make check` never touch `/tmp`. Run Go commands through `make` or set `TMPDIR=$PWD/.tmp` yourself. The build cache (`GOCACHE`) is a cache, not an artifact, and stays where the environment puts it.
6. Verify: after a task that ran tests or experiments, `git status --short` shows no new untracked paths other than those intended for the task, and `.tmp/` contains only what is still needed.

Shared `no-restricted-files` lint permits documentation only in top-level `docs/`, `.github/`, and the root `README.md`, `README_zh.md`, `AGENTS.md` and `LICENSE`. Keep temporary explanations, plans and reports in ignored `.tmp/`; never add them to the root or source directories.

- `readme-layout` requires English `README.md` and Chinese `README_zh.md`, with centered logo/h3/one-sentence introduction, language links, documentation navigation, badges, Preview/Gallery, optional Features/Why, Install/Quick Start, Star History graph and LICENSE, in that order. Keep real deployment commands and valid local links; link the Apache-2.0 `LICENSE` file in both READMEs.
- `paragraph-structure` limits Markdown paragraphs to 6 sentences and sentences to 40 whitespace-separated words. `paragraph-steps-list` requires lists for paragraphs of at least 3 sentences with sequence markers or parallel imperative actions.
- Use the shared `Button` with `size` (sm/normal/lg/xl), `variant` (primary/secondary/ghost/danger/forbidden) and `content` (text/icon-text/icon). Button lint forbids local implementations and visual overrides; icon-only buttons need accessible names.

## Backend rules

1. **errcheck** — every returned error is handled. `check-blank: true`: `_ = risky()` is a lint failure too.
2. **nilerr** — an error branch never returns a nil error.
3. **wrapcheck** — errors crossing a package boundary are wrapped with `fmt.Errorf("…: %w", err)`. Same-package returns need no wrapping.
4. **errorlint** — compare with `errors.Is`/`errors.As`; only `%w` wraps.
5. **bodyclose / sqlclosecheck** — HTTP bodies and SQL rows are closed on every path.
6. **contextcheck / containedctx** — `context.Context` is the first parameter, comes from the request or the process, and is never stored in a struct.
7. **govet lostcancel / nilness, staticcheck SA5000/SA5011/SA1019** — no leaked cancels, no nil dereferences, no deprecated APIs.
8. **forcetypeassert** — type assertions use the comma-ok form.
9. **nilnil** — no `(nil, nil)` for pointer/interface + error pairs; a deliberate exception carries a `//nolint:nilnil` with the reason.
10. **exhaustive** — switches over iota enums cover every value or have an explicit default.
11. **unused / ineffassign** — no unused code or dead assignments.
12. **No comments** — code explains itself. The only comments allowed document an exported identifier, a non-obvious constraint of an external system, or a `//nolint` directive.
13. **forbidigo** — no `fmt.Print*`, no `log.Fatal*`, no `os.Getenv` outside `internal/config`. Use `log/slog` and return errors.
14. **godox** — no TODO/FIXME/HACK/XXX markers in committed code.
15. **Formatter** — gofumpt + goimports (`golangci-lint fmt`).

`make lint` runs all of the above.

## Frontend rules

1. **Vue 3 composition API.** Components live in `web/ui/src`, use `<script setup>`, reactive state and props. Pages share `DialogPanel.vue` and `workspace.ts`; no Alpine directives or direct DOM mutation.
2. **Inputs live in dialogs.**

   - Native `<select>` / `<option>` selectors are forbidden by template lint; use `DropdownSelect` for selection fields.
   - All inputs, dropdown selectors and textareas belong in the native shared `<dialog>`.
   - Page bodies carry buttons and read-only views.
   - Native `showModal()` provides focus containment, Escape and focus restoration; busy submissions cannot be cancelled.
   - Native `<input type="checkbox">` is forbidden, including bound types and attribute spreads that cannot rule out checkbox inputs.
   - Use `CheckboxField` for boolean or multiple-choice fields inside dialogs.
   - Dropdowns use `DropdownMenu` or `DropdownSelect`.
   - Their panels use the shared `.dropdown-panel` fixed positioning and `useDropdownPosition`; opening a dropdown must not insert content into normal flow, resize surrounding content or change a parent's overflow.
   - Template and CSS lint reject custom panels and position overrides, including responsive overrides.
3. **Reuse styles.** Use the `.card`, `.btn`, `.field`, `.input`, `.table`, `.badge` and `.tabs-trigger` classes in `web/ui/theme.css`. Avoid decorative wrappers and duplicate state markup.
4. **Messages are toasts.** Use `notify()` from `web/ui/src/toast.ts` or `page.report(err)`. No inline alerts, tooltips or popovers. Dialog validation remains inert `.field` text. Server flash text is already translated and delivered in the JSON payload. Ding receives the per-request CSP style nonce.
5. **Vue owns the DOM.** Oxlint's `center/no-page-dom-access` bans document calls and HTML sinks in scripts. Only `main.ts` reads `#page-data` once. Component template refs may call native dialog methods; do not query or rewrite page DOM.
6. **Template checks.** Oxlint JS plugins receive script ASTs, not Vue template nodes. `tools/oxlint/templates.ts`, exercised against every SFC by Vitest, checks dialog-only inputs, inline messages/icons, unsafe HTML/URLs, unprotected new-tab links, raw DOM expressions, control names and expression length. These checks are deliberately separate from Oxlint; they are not a full accessibility audit. Review keyboard behavior and labels as well.
7. **Short templates.** Directive expressions are at most 50 characters. Move behavior and complex conditions to named functions or computed state. Component files stay below 400 nonblank lines.
8. **Translations.** Components use `msg(id, vars)` from `web/ui/src/i18n.ts`. Both English and Chinese catalogs in `internal/i18n/locales` contain every ID; the same catalogs supply frontend fallbacks and Go page messages. Keep `/language?lang=` and the header language menu. Do not retranslate server flash text.
9. **Icons.** Use Remix Icon classes with `aria-hidden="true"`. No SVG markup or emoji in templates. The favicon and vendored icon font remain static assets.
10. **Types and formatting.**

    - JavaScript/TypeScript scripts must use `.ts`; every Vue script block must use `lang="ts"`.
    - Shared repository lint rejects other script extensions, including `.mjs`.
    - TypeScript modules use native type annotations.
    - `npm run typecheck` runs TypeScript 7 for modules, tools and non-component tests, and vue-tsc with its TypeScript 6 compatibility dependency for Vue templates and component tests.
    - Cast untyped API payloads explicitly at boundaries.
    - Vite Plus provides Oxlint, Oxfmt, Vitest and the production build.
    - `npm run lint`, `npm run typecheck`, `npm test` and `npm run build` must pass.
11. **Artifacts.**

    - Vite outputs only `.tmp/ui`; Shiki and Markdown rendering output `.tmp/docs`.
    - Go embeds both directories.
    - Run `make assets` before direct Go tooling after a clean checkout.
    - `make test`, `make lint`, `make check` and `make build` prepare it automatically.
    - Keep `.tmp/ui` and `.tmp/docs` for subsequent builds; binaries belong in `.tmp/bin`.
    - No generated bundles are committed.

## Testing

- `go test ./...` — OAuth login through a fake identity provider, project isolation, CSRF.
- `go test ./...` in `sdk/configcenter`; the live test runs only with `CC_TEST_URL` and `CC_TEST_KEY`.
- `npm test` — Vitest and Vue Test Utils with jsdom cover dialogs, requests, permissions, translations, toast setup and template rules.
- `internal/server/e2e_test.go` renders every page, logs in through a fake IdP and proves a key of one project cannot reach another. Its `TestMain` (`scratch_test.go`) points `TMPDIR` at `.tmp/go-test` so `go test` run directly, without `make`, also keeps its scratch files in the workspace.
- Relational test databases use `mode=memory`. SDK caches use isolated in-memory maps with TTL; dotenv test files live under the repository root `.tmp/` and are cleaned up. No database is written into a package directory.

## Definition of done

`make check` passes: Vite Plus production build, Go lint and tests, Oxlint, strict Vue type checks, Vitest, and SDK lint and tests. `make build` produces `.tmp/bin/configcenter`. Commit working changes according to `docs/commits.md` and finish with a clean `git status`. Build artifacts remain ignored under `.tmp/`.

## Dangerous UI actions

Signing out is a dangerous operation, including controls labeled `退出登录`, `Sign out`, `Logout` or `nav.signOut`.

- Register dangerous operations in `web/ui/src/danger.ts`.
- Construct actions with `dangerAction()` so execution is deferred to the shared confirmation dialog.
- Render them only through `DangerActions` with the translated More label; it uses `DropdownMenu` and `.btn-danger`.
- All secondary menus use `DropdownMenu`.
- `npm run lint` checks every Vue template for dangerous labels/markers, custom secondary menus, shared styles and the confirmation request handler.
- New operations must add an explicit registry ID; arbitrary handler aliases cannot be inferred from text alone.

Icon buttons (icon children, `IconButton`, `icon` props or `data-icon-button`) may show at most 4 Unicode characters when the text contains Han characters, otherwise 10. Whitespace and punctuation count after whitespace normalization and trimming. Every translation and statically resolvable computed branch is checked by `npm run lint`; unresolved dynamic labels fail. Accessible names for icon-only buttons are exempt from this visible-text limit.

## Shared infrastructure

- `common.lock.json` pins the independently maintained center-common repository. Run `make bootstrap` before using a fresh checkout. `make common-local COMMON_DIR=../center-common` enables live local sharing; `CI=true make bootstrap check build` validates the pinned dependency.
- Shared OAuth, static asset serving, base controls, styles, Vite configuration, lint rules and CI actions live in center-common. Change them there; do not restore local copies. Business policies and adapters stay here.
- Shared components import from `@oxeu/center-common/ui/*`; the public Go module is `github.com/OXeu/center-common`. `.tmp/common` is the sole local/pinned dependency entry point.
- The common bootstrap is `tools/common.ts`, synchronized with common's `tools/bootstrap.ts`. CI checks they match. Keep both lockfiles and common revision updates together when changing dependencies.
