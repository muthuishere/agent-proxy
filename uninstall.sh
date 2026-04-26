#!/usr/bin/env bash
set -euo pipefail

# ── AgentProxy uninstaller for macOS and Linux ────────────────────────────
# Usage:
#   ./uninstall.sh              # remove binary, certs, runtime dirs, and OS CA trust
#   ./uninstall.sh --keep-trust # keep OS CA trust, remove local files and commands only

cd "$(dirname "${BASH_SOURCE[0]}")"

KEEP_TRUST=0
for arg in "$@"; do
  case "$arg" in
    --trust) ;; # Backward-compatible no-op: trust removal is now the default.
    --keep-trust) KEEP_TRUST=1 ;;
    -h|--help)
      echo "Usage: ./uninstall.sh [--keep-trust]"
      echo "Default: remove installed commands, local cert files, runtime cert cache, and OS CA trust."
      echo "  --keep-trust   Skip OS trust-store removal."
      exit 0
      ;;
    *)
      echo "Unknown option: $arg" >&2
      echo "Usage: ./uninstall.sh [--keep-trust]" >&2
      exit 1
      ;;
  esac
done

CA_CERT="$HOME/.agentproxy/certs/agentproxy-ca-cert.pem"

# ── Remove OS trust ───────────────────────────────────────────────────────────
remove_macos_trust() {
  local fingerprint=""
  if [ -f "$CA_CERT" ] && command -v openssl >/dev/null 2>&1; then
    fingerprint="$(openssl x509 -in "$CA_CERT" -noout -fingerprint -sha1 2>/dev/null | cut -d= -f2 | tr -d ':' | tr '[:lower:]' '[:upper:]')"
  fi

  echo "Removing CA cert from macOS System Keychain ..."
  if [ -n "$fingerprint" ]; then
    sudo security delete-certificate -Z "$fingerprint" /Library/Keychains/System.keychain 2>/dev/null \
      && echo "Removed CA cert from System Keychain." \
      || echo "CA cert not found in System Keychain (may have been removed already)."
    return
  fi

  sudo security delete-certificate -c "goproxy.github.io" /Library/Keychains/System.keychain 2>/dev/null \
    && echo "Removed legacy GoProxy CA cert from System Keychain." \
    || echo "CA cert not found in System Keychain (may have been removed already)."
}

remove_linux_trust() {
  local removed=0
  echo "Removing CA cert from Linux trust stores ..."
  for target in \
    /usr/local/share/ca-certificates/agentproxy-ca.crt \
    /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt
  do
    if [ -e "$target" ]; then
      sudo rm -f "$target"
      echo "Removed $target"
      removed=1
    fi
  done

  if [ "$removed" -eq 0 ]; then
    echo "CA cert not found in known Linux trust-store paths (may have been removed already)."
    return
  fi

  if command -v update-ca-certificates >/dev/null 2>&1; then
    sudo update-ca-certificates
  elif command -v update-ca-trust >/dev/null 2>&1; then
    sudo update-ca-trust
  else
    echo "Trust store file removed. No trust refresh command found; restart affected applications if needed."
  fi
}

if [ "$KEEP_TRUST" -eq 1 ]; then
  echo "Keeping OS CA trust because --keep-trust was supplied."
else
  OS=$(uname -s)
  case "$OS" in
    Darwin) remove_macos_trust ;;
    Linux) remove_linux_trust ;;
    *) echo "OS trust removal not implemented for $OS; continuing with local file removal." ;;
  esac
fi

# ── Remove CA cert directory ──────────────────────────────────────────────────
CERT_DIR="$HOME/.agentproxy/certs"
if [ -d "$CERT_DIR" ]; then
  rm -rf "$CERT_DIR"
  echo "Removed $CERT_DIR"
fi

# ── Remove local runtime directories ─────────────────────────────────────────
if [ -d "./certs" ]; then
  rm -rf ./certs
  echo "Removed ./certs"
fi

# ── Remove binary ─────────────────────────────────────────────────────────────
if [ -f "./agentproxy" ]; then
  rm -f ./agentproxy
  echo "Removed ./agentproxy"
fi

# ── Remove installed commands from ~/.local/bin ───────────────────────────────
INSTALL_DIR="$HOME/.local/bin"
for cmd in agentproxy claudeproxy codexproxy copilotproxy agentproxy-start; do
  if [ -e "$INSTALL_DIR/$cmd" ]; then
    rm -f "$INSTALL_DIR/$cmd"
    echo "Removed $INSTALL_DIR/$cmd"
  fi
done

echo ""
echo "AgentProxy uninstalled. The ./logs directory was left in place."
