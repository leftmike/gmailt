# gmailt

A command line tool for managing Gmail: list, read, label, archive, and delete messages, and
undo those changes. It is modeled on [pop3t](https://github.com/leftmike/pop3t), but uses the
Gmail API with OAuth instead of POP3 and passwords.

## Install

```
go install github.com/leftmike/gmailt@latest
```

## Google Cloud setup

gmailt needs an OAuth client of your own. This is a one-time setup:

1. In the [Google Cloud console](https://console.cloud.google.com/), create a project and
   enable the **Gmail API** for it.
2. Configure the **OAuth consent screen**: choose **External** and add your Gmail address as a
   test user.
3. Under **Credentials**, create an **OAuth client ID** of type **Desktop app**, download its
   JSON file, and save it as `~/.gmailt/credentials.json`.
4. Run `gmailt auth`. It prints a URL; open it in a browser, sign in, and allow access. The
   token is saved to `~/.gmailt/token.json` (mode `0600`).

While the app's publishing status is **Testing**, Google expires the token after 7 days and
gmailt will ask you to run `gmailt auth` again. Setting the status to **In production** avoids
this; since the app is unverified, Google shows an "unverified app" warning once, which you can
click through for your own use.

gmailt asks only for the `gmail.modify` scope: it can read, label, and trash messages, but it
can't permanently delete them. Gmail empties the trash after 30 days.

## Usage

```
gmailt <cmd> [flags] [args]
```

| Command | Description |
| --- | --- |
| `list [-n <max>] [<query>...]` | List messages with id, date, sender, and subject; `*` marks unread. |
| `get [-format brief\|normal\|full\|raw\|parts\|labels] <id>...\|<query>...` | Display messages. |
| `label [-create] +<label>\|^<label>... <id>...\|<query>...` | Add (`+`) and remove (`^`) labels. |
| `archive <id>...\|<query>...` | Remove messages from the inbox. |
| `delete <id>...\|<query>...` | Move messages to the trash (alias `trash`). |
| `labels` | List labels with message and unread counts. |
| `undo [<n>]` | Undo the most recent `label`, `archive`, or `delete`, or entry `<n>` from `history`. |
| `history` | Show the commands that `undo` can reverse. |
| `auth [-revoke]` | Authorize gmailt, or revoke and delete the saved token. |
| `config` | Show the effective configuration. |
| `help` | Show help. |

### Selecting messages

Ids are the 16 hex digit Gmail message ids shown by `list`. If every argument is an id, the
command acts on those messages; otherwise the arguments are joined into a
[Gmail search query](https://support.google.com/mail/answer/7190), the same as the search box
in Gmail. Mixing ids and query words is an error.

```
gmailt list                              # in:inbox, newest 50
gmailt list is:unread in:inbox
gmailt list -n 0 label:receipts older_than:1y
gmailt get 18f3a2b4c5d6e7f8
gmailt archive from:newsletter@example.com
gmailt delete in:spam older_than:30d
```

`list` defaults to `in:inbox` and shows at most 50 messages unless `-n` is given (`-n 0` for no
limit). The other commands act on every matching message, so preview a query with
`list -n 0` first.

Flags go before the query. A query starting with `-` (negation) needs `--` first, and shell
characters such as `(`, `)`, and `"` need quoting:

```
gmailt list -- -in:inbox -in:sent
gmailt archive 'subject:"weekly report"'
```

### Labels

`label` takes one or more leading `+<label>` (add) and `^<label>` (remove) arguments, followed
by ids or a query. Label names are matched without regard to case; names with spaces need
quoting. A label that doesn't exist is an error unless `-create` is given for a `+` label.
Gmail's system labels work too:

| To | Use |
| --- | --- |
| mark as spam | `label +spam ^inbox <query>` |
| mark as read | `label ^unread <query>` |
| mark as unread | `label +unread <query>` |
| star | `label +starred <query>` |
| archive | `label ^inbox <query>` (or `archive <query>`) |

```
gmailt label +receipts ^inbox from:orders@example.com
gmailt label -create +"Assisted Living" 18f3a2b4c5d6e7f8
```

### Undo

`label`, `archive`, and `delete` record what they actually changed in a history file, using
Gmail's own change history, so a message that already had a label isn't affected when the
command is undone. `history` lists the recorded commands, newest first:

```
$ gmailt history
   2  2026-11-22 18:08       3  delete from:old@example.com
   1  2026-11-22 18:06      28  archive in:inbox  (undone)
```

`undo` reverses the most recent command that hasn't been undone, and `undo <n>` reverses entry
`<n>`. Deleted messages are restored from the trash. Labels created with `-create` are left in
place. Entries older than 30 days are dropped.

## Configuration

Settings are read from the first of these files that exists, or from `-config <file>`:

- `~/.gmailt/gmailt.hcl` (`~/gmailt/gmailt.hcl` on Windows)
- `~/.gmailt.hcl`
- `./gmailt.hcl`

```hcl
credentials = "~/.gmailt/credentials.json"
token       = "~/.gmailt/token.json"
history     = "~/.gmailt/history.jsonl"
```

These are the defaults, so a config file is only needed to change them. Each can also be given
as a flag (`-credentials`, `-token`, `-history`), which takes precedence over the config file.
`gmailt config` shows the values in effect.

`-v` (or `-verbose`) prints each Gmail API call, with its status and time, to stderr.
