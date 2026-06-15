# Contributing to NuraOS

## Branch naming

Every unit of work lives on its own branch. The naming convention is:

```
phase-NN-short-slug
```

For example: `phase-00-bootstrap`, `phase-03-kernel-config`, `phase-17-provider-local`.

Never push directly to `main`. Open a pull request from the phase branch and
merge only after the acceptance checklist for that phase is satisfied.

## Commit format

Use conventional commits. Each commit must fit within a 3-15 minute unit of
independent, testable work.

Allowed prefixes:

| Prefix      | Use for                                                      |
|-------------|--------------------------------------------------------------|
| `feat:`     | a new capability added to the system                         |
| `fix:`      | a bug fix                                                    |
| `chore:`    | maintenance work that does not affect runtime behaviour      |
| `docs:`     | documentation only                                           |
| `build:`    | build scripts, toolchain, CI config                          |
| `test:`     | tests only                                                   |
| `refactor:` | restructuring with no behaviour change                       |
| `ci:`       | CI pipeline changes                                          |
| `perf:`     | performance improvements                                     |

Subject line rules:
- Imperative mood: "add X", not "added X" or "adds X".
- No period at the end of the subject.
- Keep to 72 characters or fewer.
- Body lines wrap at 72 characters.

## Style rules (enforced by CI)

- No em dash (`—`) anywhere: code, comments, docs, commit messages.
- Prefer "explore" or "investigate" over "experience" in prose.
- Secrets are never committed. Provider API keys, gateway tokens, and private
  keys go in `/data/etc/secrets.toml` or environment variables only.

## Git identity

Commits must carry the correct identity:

```
git config user.name "YASSERRMD"
git config user.email "arafath.yasser@gmail.com"
```

## Pull request checklist

Before opening a PR from a phase branch:

1. Run the language checks for every changed language:
   - Rust: `cargo fmt --check && cargo clippy -- -D warnings && cargo test`
   - Go: `go vet ./... && go test ./... && go build ./...`
2. Run the em-dash scan across changed files (CI will enforce this).
3. Confirm the phase build target succeeds and QEMU boots to the expected state.
4. Satisfy every item in the phase acceptance checklist.
5. Verify the commit history is atomic with conventional messages.
6. Confirm no secrets appear in any staged file.
7. Describe what was verified in the PR body and link the phase.

## Local-first principle

The default boot path must work with no network access. Remote providers
(Anthropic, OpenAI-compatible) are opt-in. No remote call is ever made on
behalf of the user without explicit configuration.
