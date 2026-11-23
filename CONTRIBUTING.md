# Contributing to LANcast

Thank you for looking. Please read this before you open a pull request, because
one rule here cannot be undone afterwards.

## Bug reports, ideas and questions: just open an issue

These need no agreement of any kind, and they are worth as much as code. A good
report says which version you are on (Settings › About), what you did, what you
expected, and what happened instead. If you can, include the matching lines from
`lancastd.log`. Since v0.9.48 the log says *why* something was refused or
decided.

Security problems are different. Please follow [SECURITY.md](SECURITY.md)
rather than opening a public issue.

## Code: talk first, and expect a CLA

LANcast is offered under two licences: the AGPL-3.0 and a commercial licence
([COMMERCIAL.md](COMMERCIAL.md)). That only works because the copyright is
undivided. The first patch merged without a contributor licence agreement ends
the commercial option permanently, for everyone.

So:

1. **Open an issue before writing code** and describe what you want to change.
   Whether it fits, and how it should be built, is cheaper to settle before
   the work is done.
2. **A pull request cannot be merged until a contributor licence agreement is
   signed.** The agreement is arranged on the issue. It is not a judgement of
   the contribution, only the one thing that keeps the dual licence possible.
3. Pull requests opened without either of those will be closed politely,
   however good they are.

## If you do write code

Read [CLAUDE.md](CLAUDE.md) (the project's working rules) and the relevant
records in [docs/adr/](docs/adr/) first. In particular:

- An API change updates `docs/api.md` **and** `docs/openapi.json` in the same
  commit. The tests enforce this.
- A field a person edited is never overwritten by a scan or a metadata refresh.
- Anything that touches the UI follows [docs/design.md](docs/design.md). Gold
  means *where you are*, and nothing else.

Before asking for review, all three of these must pass:

```bash
go test ./...
go build ./...
npm --prefix web test
```
