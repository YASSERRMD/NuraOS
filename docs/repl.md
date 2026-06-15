# NuraOS Serial REPL

The NuraOS agent includes an interactive REPL (read-eval-print loop) that
communicates over stdin/stdout. On the target device this maps to the serial
console (`ttyS0`), making it accessible without a network connection.

## Starting the REPL

```sh
nura-agent repl
```

The REPL banner is printed, then each input line is processed and a response
line is printed. Until the inference provider lands (Phase 25+), all messages
receive a stub echo response.

## Control commands

Commands prefixed with `:` are handled by the REPL itself and are never sent
to the agent core.

| Command             | Description                                     |
|---------------------|-------------------------------------------------|
| `:help`             | Show the list of available commands             |
| `:provider`         | Show the active inference provider              |
| `:provider <name>`  | Switch to a named provider (local, anthropic, openai) |
| `:tools`            | List registered tools                           |
| `:clear`            | Clear the current session (discard history)     |
| `:quit`             | Exit the REPL                                   |

Short aliases: `:h` for `:help`, `:p` for `:provider`, `:t` for `:tools`,
`:c` for `:clear`, `:q` for `:quit`.

## Session behaviour

Each line of user text starts a new **turn**. A `TurnId` (UUID v4) is assigned
at the start of the turn and appears in every log event for that turn, enabling
correlation in `/data/logs/agent.log`.

The session is retained across turns until `:clear` or `:quit`. After Phase 25
the session context (conversation history) is held in memory and is sent to the
provider with each new turn.

## Input limits

Lines longer than 64 KB are rejected with an error message and the REPL
continues. EOF (Ctrl-D) causes a clean exit. Ctrl-C (SIGINT) causes the read
call to return `Interrupted`, which is retried silently.

## Connecting via ttyS0 (QEMU)

The `run-qemu.sh` script passes `-serial mon:stdio`. To reach the REPL:

1. Boot NuraOS: `bash scripts/run-qemu.sh`
2. Wait for the supervisor to start `nura-agent`.
3. Type at the console; the REPL prompt (`> `) appears once the agent starts.

On a physical machine with a physical UART:

```sh
minicom -D /dev/ttyS0 -b 115200
```

## Architecture note

`run_repl()` in `nura-core::repl` is I/O-agnostic: it takes any `BufRead +
Write` pair, making it straightforward for the future HTTP front end (Phase
28+) to call the same `AgentCore` trait without going through the serial path.
