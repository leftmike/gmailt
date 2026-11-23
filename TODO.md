# gmailt TODO

## `send`

- `send [-subject <subject>] <addr>...`, body read from stdin, as in pop3t.
- Build the RFC822 message the same way and call `Users.Messages.Send` with a base64url `Raw`.
  No SMTP code. The From address comes from `Users.GetProfile`. The `gmail.modify` scope
  already allows sending.
- Write it the testable way (step 15 of the plan): return errors, print to `cfg.out`, and add
  `send` to the fake Gmail in `commands_test.go`.
- Add it to the README. Sent mail can't be undone, so it isn't recorded in the history.

## `forward`

- `forward <addr> <id>...|<query>...`: fetch with `getRaw`, wrap with pop3t's `wrapForward`, and
  send through `Users.Messages.Send`.
- A broad query could forward many messages, and sending can't be undone: consider a cap on how
  many messages a query may match unless a flag is given.
- Tests and README as for `send`.

## Shared package with pop3t

- Move shared code (body extraction from `message.go`, `wrapForward`) into a common package
  imported by both tools.
