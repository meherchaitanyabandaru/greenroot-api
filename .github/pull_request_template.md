## What and why

<!-- One paragraph: the problem and the change. Link the issue or ADR. -->

## Definition of Done

See `greenroot-docs/06-decisions/enterprise-engineering-standards.md` §4.

- [ ] **Design:** ADR added for any change to a contract, the schema, security or a cross-module dependency (or N/A)
- [ ] **Unit tests** for new logic
- [ ] **Integration tests** (`tests/`) for new queries or endpoints
- [ ] **Authorization tests:** allowed **and** forbidden cases for new routes
- [ ] **Migration:** forward-only, expand/contract, with a `.down.sql` (or N/A)
- [ ] **OpenAPI** updated (CI enforces this)
- [ ] **Module doc** updated if business rules changed
- [ ] **No new findings:** gofmt, vet, gosec, govulncheck; every `#nosec` has a reason

## How it was verified

<!-- Commands run, requests made, rows checked. -->

## Rollout and rollback

<!-- Config, migration order, feature flags. How to undo it. -->
