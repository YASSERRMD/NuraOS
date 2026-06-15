#!/usr/bin/env bash
# configure.sh -- Interactive NuraOS configuration helper.
#
# Sets up /data/etc/agent.toml and /data/etc/secrets.toml.
# Run this after first boot or whenever you want to change the provider.
#
# Usage:
#   ./scripts/configure.sh [--non-interactive]
#
# With --non-interactive, reads NURA_PROVIDER, NURA_GATEWAY_TOKEN,
# ANTHROPIC_API_KEY, and OPENAI_API_KEY from the environment.

set -euo pipefail

DATA_ETC="/data/etc"
AGENT_TOML="${DATA_ETC}/agent.toml"
SECRETS_TOML="${DATA_ETC}/secrets.toml"

INTERACTIVE=true
PROVIDER_CHOICE=""
GATEWAY_TOKEN_VAL=""
ANTHROPIC_KEY=""
OPENAI_KEY=""

for arg in "$@"; do
    case "${arg}" in
        --non-interactive) INTERACTIVE=false ;;
        --help|-h)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
    esac
done

log()  { printf '[configure] %s\n' "$*"; }
ask()  { printf '[configure] %s ' "$1"; read -r REPLY; echo "${REPLY}"; }
warn() { printf '[configure] WARN: %s\n' "$*" >&2; }
die()  { printf '[configure] ERROR: %s\n' "$*" >&2; exit 1; }

check_writable() {
    if ! mkdir -p "${DATA_ETC}" 2>/dev/null; then
        die "cannot create ${DATA_ETC} -- is /data mounted?"
    fi
}

# ---- Non-interactive mode: read from environment ----
if [ "${INTERACTIVE}" = "false" ]; then
    PROVIDER_CHOICE="${NURA_PROVIDER:-local}"
    GATEWAY_TOKEN_VAL="${NURA_GATEWAY_TOKEN:-}"
    ANTHROPIC_KEY="${ANTHROPIC_API_KEY:-}"
    OPENAI_KEY="${OPENAI_API_KEY:-}"
else
    log "NuraOS configuration wizard"
    log ""

    log "Available providers:"
    log "  local     -- llama-server running on-device (no cloud dependency)"
    log "  anthropic -- Anthropic Claude API (requires ANTHROPIC_API_KEY)"
    log "  openai    -- OpenAI-compatible API (requires OPENAI_API_KEY)"
    log ""
    PROVIDER_CHOICE=$(ask "Provider to use [local]:") || true
    [ -z "${PROVIDER_CHOICE}" ] && PROVIDER_CHOICE="local"

    case "${PROVIDER_CHOICE}" in
        anthropic)
            ANTHROPIC_KEY=$(ask "Anthropic API key (sk-ant-...):") || true
            [ -z "${ANTHROPIC_KEY}" ] && die "Anthropic API key is required"
            ;;
        openai)
            OPENAI_KEY=$(ask "OpenAI API key (sk-...):") || true
            [ -z "${OPENAI_KEY}" ] && die "OpenAI API key is required"
            ;;
        local)
            ;;
        *)
            die "unknown provider '${PROVIDER_CHOICE}'; choose: local, anthropic, openai"
            ;;
    esac

    GATEWAY_TOKEN_VAL=$(ask "Gateway bearer token (leave blank to disable auth):") || true
fi

check_writable

# ---- Write agent.toml ----
log "writing ${AGENT_TOML} ..."
cat > "${AGENT_TOML}" <<TOML
# NuraOS agent configuration -- managed by configure.sh

[server]
bind = "127.0.0.1"
port = 8080

[provider]
active  = "${PROVIDER_CHOICE}"
routing = "local_first"

[timeouts]
turn_secs             = 120
tool_call_secs        = 30
provider_connect_secs = 10

[token_budget]
max_context_tokens  = 4096
max_output_tokens   = 1024
max_tool_iterations = 10

log_level = "info"
TOML
chmod 644 "${AGENT_TOML}"

# ---- Write secrets.toml ----
log "writing ${SECRETS_TOML} ..."
: > "${SECRETS_TOML}"
chmod 600 "${SECRETS_TOML}"

if [ -n "${GATEWAY_TOKEN_VAL}" ]; then
    printf 'gateway_token = "%s"\n' "${GATEWAY_TOKEN_VAL}" >> "${SECRETS_TOML}"
fi
if [ -n "${ANTHROPIC_KEY}" ]; then
    printf 'anthropic_api_key = "%s"\n' "${ANTHROPIC_KEY}" >> "${SECRETS_TOML}"
fi
if [ -n "${OPENAI_KEY}" ]; then
    printf 'openai_api_key = "%s"\n' "${OPENAI_KEY}" >> "${SECRETS_TOML}"
fi

log ""
log "Configuration written:"
log "  ${AGENT_TOML}"
log "  ${SECRETS_TOML} (mode 600)"
log ""
log "Restart nura-agent and nura-gateway to apply changes."
log "To verify: nura-agent doctor"
