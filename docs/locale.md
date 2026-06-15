# NuraOS Locale and Text Encoding

NuraOS treats all text as UTF-8. No locale-specific collation, character
mapping, or string transformation is applied. The system defaults to
`C.UTF-8` so that every subsystem -- console output, log lines, HTTP API
bodies, model input/output, and provenance metadata -- uses the same encoding.

---

## Default locale

The system locale is `C.UTF-8`. This locale:

- Enforces UTF-8 encoding for all I/O.
- Applies no locale-specific collation order.
- Is supported by all Linux distributions without installing extra locale
  packages.
- Works correctly with multi-byte text (Arabic, Hebrew, CJK, etc.) and
  emoji.

---

## Configuring the locale

Override the locale by creating `/data/etc/locale`:

```sh
echo "en_US.UTF-8" > /data/etc/locale
```

Rules:

- The locale name must end in `.UTF-8` (case-insensitive). Non-UTF-8
  locales are rejected and the default `C.UTF-8` is used instead.
- Comment lines (starting with `#`) and blank lines are ignored.
- Only the first non-comment line is used.

Valid examples:

```
C.UTF-8
en_US.UTF-8
ar_AE.UTF-8
ja_JP.UTF-8
```

Invalid (rejected, falls back to default):

```
en_US.ISO-8859-1
en_US
C
POSIX
```

---

## Environment variables

Every subprocess spawned by NuraOS receives the following environment
variables to enforce UTF-8 encoding:

| Variable | Value |
|----------|-------|
| `LANG` | locale (e.g. `C.UTF-8`) |
| `LC_ALL` | locale (overrides all LC_* settings) |
| `PYTHONIOENCODING` | `utf-8` |
| `PYTHONUTF8` | `1` |

---

## Multi-byte and RTL text

NuraOS correctly handles text in all Unicode scripts, including multi-byte
sequences (Arabic, Hebrew, CJK, emoji) and right-to-left scripts.

The `locale` package provides:

- `ValidateUTF8(data []byte) bool` -- reports whether bytes are valid UTF-8.
- `SanitiseUTF8(s string) string` -- replaces invalid byte sequences with
  U+FFFD while preserving all valid code points (including RTL characters).
- `RoundTrip(s string) (string, bool)` -- verifies string survives
  encode/decode losslessly.
- `IsRTL(s string) bool` -- heuristic detection of RTL scripts (Arabic,
  Hebrew, Thaana, N'Ko) for display-layer hints.

The HTTP gateway validates that all chat request bodies are valid UTF-8.
Invalid sequences are rejected with 400 Bad Request.

---

## Serial console

The serial REPL (`/dev/ttyS0`) is configured at 115200 baud with UTF-8
encoding. Multi-byte sequences that span multiple read() calls are
buffered until complete before being forwarded. RTL text is transmitted
without any BiDi reordering; the terminal emulator is responsible for
rendering direction.

---

## Model I/O

The inference stack (llama.cpp) operates on raw UTF-8 bytes. NuraOS
sanitises model input before submission (removing lone surrogates and
invalid byte sequences) and validates model output before returning it in
HTTP responses.

---

## Note on Urdu

Urdu uses the Arabic script (Unicode block U+0600-U+06FF) and is handled
by the same UTF-8 path as Arabic. The locale does not default to any
specific regional variant; configure `ur_PK.UTF-8` in `/data/etc/locale`
to use Urdu conventions if needed.
