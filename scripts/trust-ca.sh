#!/usr/bin/env bash
set -euo pipefail

CERT_PATH="${AGENTPROXY_CA_CERT:-$HOME/.agentproxy/certs/agentproxy-ca-cert.pem}"

cert_fingerprint() {
  openssl x509 -in "$CERT_PATH" -noout -fingerprint -sha1 2>/dev/null |
    cut -d= -f2 |
    tr -d ':' |
    tr '[:lower:]' '[:upper:]'
}

is_cert_trusted() {
  case "$(uname -s)" in
    Darwin)
      local fingerprint
      fingerprint="$(cert_fingerprint)"
      [ -n "$fingerprint" ] || return 1
      security find-certificate -Z -a /Library/Keychains/System.keychain 2>/dev/null |
        tr -d ':' |
        grep -q "$fingerprint"
      ;;
    Linux)
      [ -f /usr/local/share/ca-certificates/agentproxy-ca.crt ] ||
        [ -f /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt ]
      ;;
    *)
      return 1
      ;;
  esac
}

manual_trust() {
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
    *)
      echo "Automatic CA trust is not implemented for $(uname -s)."
      ;;
  esac
}

trust_cert() {
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

if [ ! -f "$CERT_PATH" ]; then
  echo "CA cert not found at $CERT_PATH"
  echo "Run: ./agentproxy ca-setup"
  exit 1
fi

if is_cert_trusted; then
  echo "CA cert already trusted ✓"
  exit 0
fi

echo "Trusting CA cert in the OS store ..."
if trust_cert; then
  echo "CA cert trusted ✓"
else
  echo "Automatic trust failed."
  manual_trust
  exit 1
fi
