#!/usr/bin/env bash
# install-hook.sh -- Install the NuraOS pre-push git hook.
#
# The hook runs the smoke suite before any push to prevent obviously broken
# code from reaching the remote. It is intentionally lightweight: only the
# "smoke" suite runs (build-and-boot + healthz), not the full matrix.
#
# Usage:
#   tests/tools/install-hook.sh [--uninstall]
#
# Options:
#   --uninstall   Remove the hook if it exists.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
HOOK_DIR="${REPO_ROOT}/.git/hooks"
HOOK_FILE="${HOOK_DIR}/pre-push"

UNINSTALL=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --uninstall) UNINSTALL=1 ;;
        *) echo "[install-hook] unknown option: $1" >&2; exit 1 ;;
    esac
    shift
done

if [[ "${UNINSTALL}" -eq 1 ]]; then
    if [[ -f "${HOOK_FILE}" ]]; then
        rm -f "${HOOK_FILE}"
        echo "[install-hook] pre-push hook removed"
    else
        echo "[install-hook] no hook to remove"
    fi
    exit 0
fi

if [[ ! -d "${HOOK_DIR}" ]]; then
    echo "[install-hook] .git/hooks not found - are you in a git repo?" >&2
    exit 1
fi

cat > "${HOOK_FILE}" <<'HOOK'
#!/usr/bin/env bash
# NuraOS pre-push hook: runs the smoke suite before pushing.
# Skip with: git push --no-verify
set -euo pipefail
REPO_ROOT="$(git rev-parse --show-toplevel)"
echo "[pre-push] running smoke suite..."
"${REPO_ROOT}/scripts/test.sh" smoke
HOOK

chmod +x "${HOOK_FILE}"
echo "[install-hook] pre-push hook installed at ${HOOK_FILE}"
echo "[install-hook] bypass any time with: git push --no-verify"
