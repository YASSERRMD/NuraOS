# NuraOS Persona and System Prompt

## Overview

The agent's behaviour is shaped by a system prompt loaded at boot. Operators
can change the prompt without modifying or recompiling the agent binary.

## System Prompt File

The system prompt is read from:

```
/data/etc/system_prompt.md
```

If the file is absent or empty, the agent falls back to the built-in default
(documented below) and logs a warning. The file is plain text; Markdown
formatting is supported for readability but is not required.

### Boot-time validation

At boot, `PromptLoader::validate()` checks that the loaded prompt is:
- Non-empty after trimming.
- No larger than 1 MiB.

A validation failure is reported as a config error (exit code 2). The agent
will not start with an invalid prompt.

## Customising the Prompt

Edit `/data/etc/system_prompt.md` with the desired instructions, then run the
`:reload-prompt` control command from the REPL (Phase 27+) to apply the change
without restarting the agent.

**Example:**

```
/data/etc/system_prompt.md

You are the NuraOS assistant for Acme Corp infrastructure monitoring.
Focus on disk usage, load averages, and network connectivity.
When in doubt, report actual values from system tools rather than estimates.
```

## Hot-Reload

Send `:reload-prompt` at the REPL prompt to reload the file immediately:

```
nura> :reload-prompt
system prompt reloaded (512 bytes)
```

The next turn uses the updated prompt. Running turns are not interrupted.

## Persona Configuration

Persona settings are in `agent.toml`:

```toml
[persona]
verbosity = "normal"          # concise | normal | verbose
allowed_tool_categories = []  # empty = all tools allowed
```

### Verbosity

| Value | Effect |
|-------|--------|
| `concise` | Appends "Reply concisely. Prefer one or two sentences." to the prompt |
| `normal` | No modification (default) |
| `verbose` | Appends "Provide detailed answers with explanations." to the prompt |

Verbosity instructions are appended to the operator-provided (or built-in)
system prompt at runtime. They are not written back to the file.

### Tool Category Allowlist

`allowed_tool_categories` gates which tool categories the model may call.
When empty (default), all registered tools are offered. To restrict the model
to read-only tools only:

```toml
[persona]
allowed_tool_categories = ["system", "fs", "net", "time"]
```

Tool categories correspond to the prefix before the dot in the tool name
(e.g. `system.info` is category `system`, `fs.read` is category `fs`).

## Built-in Default System Prompt

When `/data/etc/system_prompt.md` is missing, the following built-in text is used:

> You are the NuraOS assistant, a local-first AI embedded directly in the
> operating system. You run entirely on-device and operate without sending
> data to external servers unless the operator has explicitly configured a
> remote provider.
>
> Your primary responsibilities:
> - Help the operator understand and manage this NuraOS appliance.
> - Execute read-only system tools when they are relevant to the operator's question.
> - Provide concise, accurate answers grounded in what you can observe locally.
>
> Guidelines:
> - Prefer brevity. The console display is narrow.
> - Do not speculate about external network services you cannot reach.
> - When a tool would help, use it rather than guessing.
> - Acknowledge uncertainty explicitly rather than fabricating information.

## Integration with Context Assembly

The system prompt is always the first message in the assembled context
(via `Role::System`). It is never dropped by the context retention policy,
regardless of how full the context window is. See [context.md](context.md) for
context window management details.
