#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
CERT_PATH="$HOME/.agentproxy/certs/agentproxy-ca-cert.pem"
INSTALL_DIR="$HOME/.local/bin"

binary_is_current() {
  [ -f "$INSTALL_DIR/agentproxy" ] && cmp -s agentproxy "$INSTALL_DIR/agentproxy"
}

link_points_to() {
  local link_path="$1"
  local expected_target="$2"
  [ -L "$link_path" ] || return 1
  [ "$(readlink "$link_path" 2>/dev/null || true)" = "$expected_target" ]
}

is_cert_trusted() {
  case "$(uname -s)" in
    Darwin)
      cert_fingerprint="$(openssl x509 -in "$CERT_PATH" -noout -fingerprint -sha1 2>/dev/null | cut -d= -f2 | tr -d ':' | tr '[:lower:]' '[:upper:]')"
      [ -n "$cert_fingerprint" ] || return 1
      security find-certificate -Z -a /Library/Keychains/System.keychain 2>/dev/null | tr -d ':' | grep -q "$cert_fingerprint"
      ;;
    Linux)
      [ -f /usr/local/share/ca-certificates/agentproxy-ca.crt ] || [ -f /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt ]
      ;;
    *)
      return 1
      ;;
  esac
}

print_manual_trust() {
  case "$(uname -s)" in
    Darwin)
      echo "Manual trust command:"
      echo "  sudo security add-trusted-cert -d -r trustRoot \\"
      echo "    -k /Library/Keychains/System.keychain \\"
      echo "    $CERT_PATH"
      ;;
    Linux)
      if command -v update-ca-certificates >/dev/null 2>&1 || [ -d /usr/local/share/ca-certificates ]; then
        echo "Manual trust commands:"
        echo "  sudo cp $CERT_PATH /usr/local/share/ca-certificates/agentproxy-ca.crt"
        echo "  sudo update-ca-certificates"
      else
        echo "Manual trust commands:"
        echo "  sudo cp $CERT_PATH /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt"
        echo "  sudo update-ca-trust"
      fi
      ;;
  esac
}

attempt_trust() {
  case "$(uname -s)" in
    Darwin)
      sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain "$CERT_PATH"
      ;;
    Linux)
      if command -v update-ca-certificates >/dev/null 2>&1 || [ -d /usr/local/share/ca-certificates ]; then
        sudo cp "$CERT_PATH" /usr/local/share/ca-certificates/agentproxy-ca.crt
        sudo update-ca-certificates
      else
        sudo cp "$CERT_PATH" /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt
        sudo update-ca-trust
      fi
      ;;
    *)
      return 1
      ;;
  esac
}

# ── 1. Check Go ──────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  echo ""
  echo "  ERROR: Go is required but was not found."
  echo "  Install Go from https://go.dev/dl/ and re-run this script."
  echo ""
  exit 1
fi

GO_VERSION=$(go version | awk '{print $3}')
echo "Using Go: $GO_VERSION"

# ── 2. Build binary ──────────────────────────────────────────────────────────
echo "Building ./agentproxy ..."
go build -o agentproxy ./cmd/agentproxy
echo "Built ./agentproxy"

REPO_DIR="$(pwd)"
preinstall_up_to_date=0
if binary_is_current && \
  link_points_to "$INSTALL_DIR/claudeproxy" "$REPO_DIR/bin/claudeproxy" && \
  link_points_to "$INSTALL_DIR/codexproxy" "$REPO_DIR/bin/codexproxy" && \
  link_points_to "$INSTALL_DIR/copilotproxy" "$REPO_DIR/bin/copilotproxy" && \
  link_points_to "$INSTALL_DIR/agentproxy-start" "$REPO_DIR/bin/agentproxy-start"; then
  preinstall_up_to_date=1
fi

# ── 3. Export CA cert and create runtime directories ─────────────────────────
echo ""
echo "Setting up CA cert and directories ..."
ca_setup_output="$(./agentproxy ca-setup)"
printf '%s\n' "$ca_setup_output"

echo ""
trust_already_present=0
if is_cert_trusted; then
  trust_already_present=1
  echo "CA cert already trusted ✓"
elif [ -t 1 ]; then
  echo "Trusting CA cert in the OS store ..."
  if attempt_trust; then
    echo "CA cert trusted ✓"
  else
    echo "Automatic trust failed."
    print_manual_trust
  fi
else
  echo "Non-interactive install detected; skipping automatic trust."
  print_manual_trust
fi

# ── 4. Install binary and wrapper scripts to ~/.local/bin ────────────────────
mkdir -p "$INSTALL_DIR"

echo ""
echo "Installing to $INSTALL_DIR ..."

if binary_is_current; then
  echo "Binary already up to date ✓"
else
  cp -f agentproxy "$INSTALL_DIR/agentproxy"
  echo "Installed binary: agentproxy"
fi

links_updated=0
for script in claudeproxy codexproxy copilotproxy agentproxy-start; do
  target="$REPO_DIR/bin/$script"
  link_path="$INSTALL_DIR/$script"
  if link_points_to "$link_path" "$target"; then
    echo "Wrapper already up to date ✓ $script"
    continue
  fi
  ln -sf "$target" "$link_path"
  echo "Installed wrapper: $script"
  links_updated=1
done

cert_already_present=0
case "$ca_setup_output" in
  *"CA cert already present ✓"*) cert_already_present=1 ;;
esac

# Warn if ~/.local/bin is not in PATH
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo ""
     echo " NOTE: $INSTALL_DIR is not in your PATH."
     echo " Add this line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
     echo "   export PATH=\"\$HOME/.local/bin:\$PATH\""
     echo " Then restart your shell or run:  source ~/.bashrc"
     ;;
esac

# ── 5. Summary ───────────────────────────────────────────────────────────────
echo ""
echo "──────────────────────────────────────────────────────"
if [ "$preinstall_up_to_date" -eq 1 ] && [ "$cert_already_present" -eq 1 ] && [ "$trust_already_present" -eq 1 ] && [ "$links_updated" -eq 0 ]; then
  echo " Install already up to date ✓"
else
  echo " AgentProxy installed successfully."
fi
echo ""
echo " Start the proxy:    agentproxy-start"
echo " Claude via proxy:   claudeproxy ..."
echo " Codex via proxy:    codexproxy ..."
echo " Copilot via proxy:  copilotproxy ..."
echo ""
echo "──────────────────────────────────────────────────────"
