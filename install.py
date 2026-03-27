#!/usr/bin/env python3
"""AgentProxy — one-command installer and uninstaller.

Works on macOS, Linux, and Windows. Requires Python 3.11+.

Usage:
    python install.py            # install
    python install.py uninstall  # remove everything
"""
import os
import platform
import shutil
import subprocess
import sys
import textwrap
import time
from pathlib import Path

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

REPO_DIR   = Path(__file__).parent.resolve()
SYSTEM     = platform.system()          # "Darwin", "Linux", "Windows"
IS_WINDOWS = SYSTEM == "Windows"
IS_MAC     = SYSTEM == "Darwin"
IS_LINUX   = SYSTEM == "Linux"

CERT_PATH  = Path.home() / ".mitmproxy" / "mitmproxy-ca-cert.pem"

# Where wrapper scripts are installed
if IS_WINDOWS:
    BIN_DIR = Path.home() / ".local" / "bin"
else:
    BIN_DIR = Path.home() / ".local" / "bin"

WRAPPERS = ["agentproxy-start", "claudeproxy", "codexproxy", "copilotproxy"]

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _banner(text: str) -> None:
    width = 60
    print()
    print("=" * width)
    print(f"  {text}")
    print("=" * width)


def _step(text: str) -> None:
    print(f"\n  ▶  {text}")


def _ok(text: str) -> None:
    print(f"     ✓  {text}")


def _warn(text: str) -> None:
    print(f"     ⚠  {text}")


def _info(text: str) -> None:
    print(f"     •  {text}")


def _run(cmd: list[str], *, check: bool = True, capture: bool = False,
         cwd: Path | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(
        cmd,
        check=check,
        capture_output=capture,
        cwd=cwd or REPO_DIR,
    )


def _which(name: str) -> str | None:
    return shutil.which(name)

# ---------------------------------------------------------------------------
# Step 1 — Python version check
# ---------------------------------------------------------------------------

def check_python() -> None:
    _step("Checking Python version")
    if sys.version_info < (3, 11):
        print(f"\n  ERROR: Python 3.11+ required, got {sys.version}")
        print("  Download from https://www.python.org/downloads/")
        sys.exit(1)
    _ok(f"Python {sys.version.split()[0]}")

# ---------------------------------------------------------------------------
# Step 2 — uv
# ---------------------------------------------------------------------------

def ensure_uv() -> None:
    _step("Checking for uv")
    if _which("uv"):
        result = _run(["uv", "--version"], capture=True, check=False)
        version = result.stdout.decode().strip() if result.stdout else "uv"
        _ok(version)
        return

    _info("uv not found — installing now")
    if IS_WINDOWS:
        _run([
            "powershell", "-ExecutionPolicy", "ByPass",
            "-c", "irm https://astral.sh/uv/install.ps1 | iex",
        ])
    else:
        _run(["sh", "-c", "curl -LsSf https://astral.sh/uv/install.sh | sh"])

    # After install, uv may be in ~/.cargo/bin or ~/.local/bin — reload PATH
    for candidate in [
        Path.home() / ".local" / "bin" / "uv",
        Path.home() / ".cargo" / "bin" / "uv",
    ]:
        if candidate.exists():
            os.environ["PATH"] = str(candidate.parent) + os.pathsep + os.environ.get("PATH", "")
            break

    if not _which("uv"):
        print("\n  ERROR: uv installation succeeded but 'uv' is not in PATH.")
        print("  Restart your shell and re-run install.py.")
        sys.exit(1)

    _ok("uv installed")

# ---------------------------------------------------------------------------
# Step 3 — Install Python dependencies
# ---------------------------------------------------------------------------

def sync_deps() -> None:
    _step("Installing Python dependencies (uv sync)")
    _run(["uv", "sync"])
    _ok("Dependencies ready")

# ---------------------------------------------------------------------------
# Step 4 — Generate mitmproxy CA certificate
# ---------------------------------------------------------------------------

def generate_cert() -> None:
    _step("Generating mitmproxy CA certificate")

    if CERT_PATH.exists():
        _ok(f"Certificate already exists: {CERT_PATH}")
        return

    _info("Starting proxy briefly to generate cert...")
    # Run mitmdump on a throwaway port for 3 seconds so it writes the cert
    proc = subprocess.Popen(
        ["uv", "run", "mitmdump", "--listen-port", "19876"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        cwd=REPO_DIR,
    )
    deadline = time.time() + 10
    while not CERT_PATH.exists() and time.time() < deadline:
        time.sleep(0.3)
    proc.terminate()
    proc.wait(timeout=5)

    if not CERT_PATH.exists():
        _warn("Certificate not generated — mitmproxy may need to run once manually.")
        _info("Run: uv run mitmdump --listen-port 7717")
        _info("Then press Ctrl-C and re-run install.py")
    else:
        _ok(f"Certificate generated: {CERT_PATH}")

# ---------------------------------------------------------------------------
# Step 5 — Trust the CA certificate
# ---------------------------------------------------------------------------

def trust_cert() -> None:
    _step("Trusting mitmproxy CA certificate")

    if not CERT_PATH.exists():
        _warn("Skipping — certificate not found")
        return

    if IS_MAC:
        _trust_mac()
    elif IS_LINUX:
        _trust_linux()
    elif IS_WINDOWS:
        _trust_windows()


def _trust_mac() -> None:
    result = _run([
        "security", "add-trusted-cert",
        "-d", "-r", "trustRoot",
        "-k", str(Path.home() / "Library" / "Keychains" / "login.keychain-db"),
        str(CERT_PATH),
    ], check=False)
    if result.returncode == 0:
        _ok("Certificate trusted in macOS login keychain")
    else:
        # Fallback: try without explicit keychain path
        result2 = _run([
            "security", "add-trusted-cert", "-d", "-r", "trustRoot",
            str(CERT_PATH),
        ], check=False)
        if result2.returncode == 0:
            _ok("Certificate trusted in macOS keychain")
        else:
            _warn("Could not trust certificate automatically — do it manually:")
            _info("  Open Keychain Access → drag in ~/.mitmproxy/mitmproxy-ca-cert.pem")
            _info("  Double-click it → Trust → 'Always Trust' for SSL")


def _trust_linux() -> None:
    # Try the most common distro locations
    ubuntu_dir  = Path("/usr/local/share/ca-certificates")
    rhel_dir    = Path("/etc/pki/ca-trust/source/anchors")
    arch_dir    = Path("/etc/ca-certificates/trust-source/anchors")

    if ubuntu_dir.exists():
        dest = ubuntu_dir / "agentproxy-ca.crt"
        try:
            result = _run(["sudo", "cp", str(CERT_PATH), str(dest)], check=False)
            if result.returncode == 0:
                _run(["sudo", "update-ca-certificates"], check=False)
                _ok("Certificate trusted (Ubuntu/Debian)")
                return
        except Exception:
            pass

    if rhel_dir.exists():
        dest = rhel_dir / "agentproxy-ca.crt"
        try:
            result = _run(["sudo", "cp", str(CERT_PATH), str(dest)], check=False)
            if result.returncode == 0:
                _run(["sudo", "update-ca-trust", "extract"], check=False)
                _ok("Certificate trusted (RHEL/CentOS)")
                return
        except Exception:
            pass

    if arch_dir.exists():
        dest = arch_dir / "agentproxy-ca.crt"
        try:
            result = _run(["sudo", "cp", str(CERT_PATH), str(dest)], check=False)
            if result.returncode == 0:
                _run(["sudo", "trust", "extract-compat"], check=False)
                _ok("Certificate trusted (Arch)")
                return
        except Exception:
            pass

    _warn("Could not trust certificate automatically — do it manually:")
    _info(f"  Copy {CERT_PATH} to your distro's trusted CA directory")
    _info("  Then run the appropriate update command (update-ca-certificates, update-ca-trust, etc.)")


def _trust_windows() -> None:
    result = _run([
        "certutil", "-addstore", "-user", "Root", str(CERT_PATH),
    ], check=False)
    if result.returncode == 0:
        _ok("Certificate trusted in Windows user certificate store")
    else:
        _warn("Could not trust certificate automatically.")
        _info("Run as Administrator:")
        _info(f'  certutil -addstore Root "{CERT_PATH}"')

# ---------------------------------------------------------------------------
# Step 6 — Install wrapper scripts
# ---------------------------------------------------------------------------

def install_wrappers() -> None:
    _step(f"Installing wrapper scripts to {BIN_DIR}")
    BIN_DIR.mkdir(parents=True, exist_ok=True)

    if IS_WINDOWS:
        _install_wrappers_windows()
    else:
        _install_wrappers_unix()

    _ok(f"Wrapper scripts installed in {BIN_DIR}")
    _check_path()


def _install_wrappers_unix() -> None:
    for name in WRAPPERS:
        src = REPO_DIR / "bin" / name
        dst = BIN_DIR / name
        shutil.copy2(src, dst)
        dst.chmod(0o755)


def _install_wrappers_windows() -> None:
    """Generate .cmd wrapper files for Windows."""
    host = "127.0.0.1"
    port = "7717"
    cert = str(Path.home() / ".mitmproxy" / "mitmproxy-ca-cert.pem")
    repo = str(REPO_DIR)

    wrappers_cmd = {
        "agentproxy-start.cmd": textwrap.dedent(f"""\
            @echo off
            set CONFIG_FILE={repo}\\config\\agentproxy.yaml
            cd /d "{repo}"
            uv run mitmdump --listen-host {host} --listen-port {port} -s proxy\\addon.py --ssl-insecure
        """),
        "claudeproxy.cmd": textwrap.dedent(f"""\
            @echo off
            set HTTP_PROXY=http://{host}:{port}
            set HTTPS_PROXY=http://{host}:{port}
            set AGENTPROXY_CA_CERT={cert}
            set NODE_EXTRA_CA_CERTS=%AGENTPROXY_CA_CERT%
            set SSL_CERT_FILE=%AGENTPROXY_CA_CERT%
            set REQUESTS_CA_BUNDLE=%AGENTPROXY_CA_CERT%
            set CURL_CA_BUNDLE=%AGENTPROXY_CA_CERT%
            claude %*
        """),
        "codexproxy.cmd": textwrap.dedent(f"""\
            @echo off
            set HTTP_PROXY=http://{host}:{port}
            set HTTPS_PROXY=http://{host}:{port}
            set AGENTPROXY_CA_CERT={cert}
            set NODE_EXTRA_CA_CERTS=%AGENTPROXY_CA_CERT%
            set SSL_CERT_FILE=%AGENTPROXY_CA_CERT%
            set REQUESTS_CA_BUNDLE=%AGENTPROXY_CA_CERT%
            set CURL_CA_BUNDLE=%AGENTPROXY_CA_CERT%
            codex %*
        """),
        "copilotproxy.cmd": textwrap.dedent(f"""\
            @echo off
            set HTTP_PROXY=http://{host}:{port}
            set HTTPS_PROXY=http://{host}:{port}
            set AGENTPROXY_CA_CERT={cert}
            set NODE_EXTRA_CA_CERTS=%AGENTPROXY_CA_CERT%
            set SSL_CERT_FILE=%AGENTPROXY_CA_CERT%
            set REQUESTS_CA_BUNDLE=%AGENTPROXY_CA_CERT%
            set CURL_CA_BUNDLE=%AGENTPROXY_CA_CERT%
            where copilot >nul 2>nul
            if %ERRORLEVEL% EQU 0 (
              copilot %*
            ) else (
              gh copilot %*
            )
        """),
    }

    for filename, content in wrappers_cmd.items():
        dst = BIN_DIR / filename
        dst.write_text(content)


def _check_path() -> None:
    paths = os.environ.get("PATH", "").split(os.pathsep)
    if str(BIN_DIR) not in paths:
        _warn(f"{BIN_DIR} is not in your PATH")
        if IS_WINDOWS:
            _info('Add it permanently:')
            _info(f'  setx PATH "%PATH%;{BIN_DIR}"')
            _info("  (Restart your terminal after running that)")
        else:
            shell_rc = _detect_shell_rc()
            _info(f'Add to {shell_rc}:')
            _info(f'  export PATH="$HOME/.local/bin:$PATH"')
    else:
        _ok("PATH is set correctly")


def _detect_shell_rc() -> str:
    shell = os.environ.get("SHELL", "")
    if "zsh" in shell:
        return "~/.zshrc"
    if "fish" in shell:
        return "~/.config/fish/config.fish"
    return "~/.bashrc"

# ---------------------------------------------------------------------------
# Uninstall
# ---------------------------------------------------------------------------

def remove_wrappers() -> None:
    _step(f"Removing wrapper scripts from {BIN_DIR}")
    removed = []

    if IS_WINDOWS:
        names = [f"{w}.cmd" for w in WRAPPERS]
    else:
        names = WRAPPERS

    for name in names:
        path = BIN_DIR / name
        if path.exists():
            path.unlink()
            removed.append(name)

    if removed:
        _ok(f"Removed: {', '.join(removed)}")
    else:
        _info("No wrapper scripts found to remove")


def untrust_cert() -> None:
    _step("Removing CA certificate trust")

    if IS_MAC:
        result = _run([
            "security", "delete-certificate",
            "-c", "mitmproxy",
        ], check=False)
        if result.returncode == 0:
            _ok("Certificate removed from macOS keychain")
        else:
            _warn("Could not remove automatically — check Keychain Access manually")

    elif IS_LINUX:
        for path in [
            Path("/usr/local/share/ca-certificates/agentproxy-ca.crt"),
            Path("/etc/pki/ca-trust/source/anchors/agentproxy-ca.crt"),
            Path("/etc/ca-certificates/trust-source/anchors/agentproxy-ca.crt"),
        ]:
            if path.exists():
                _run(["sudo", "rm", "-f", str(path)], check=False)
        _run(["sudo", "update-ca-certificates", "--fresh"], check=False)
        _ok("Certificate removed (Linux)")

    elif IS_WINDOWS:
        result = _run(
            ["certutil", "-delstore", "-user", "Root", "mitmproxy"],
            check=False,
        )
        if result.returncode == 0:
            _ok("Certificate removed from Windows store")
        else:
            _warn('Run as Administrator: certutil -delstore Root mitmproxy')

# ---------------------------------------------------------------------------
# Print success summary
# ---------------------------------------------------------------------------

def print_success() -> None:
    print()
    print("=" * 60)
    print("  AgentProxy installed successfully!")
    print("=" * 60)
    print()
    print("  Start the proxy:")
    print("    agentproxy-start")
    print()
    print("  Run agents through the proxy:")
    print("    claudeproxy   claude  'your prompt'")
    print("    codexproxy    codex   'your prompt'")
    print("    copilotproxy  gh copilot suggest 'your prompt'")
    print()
    print("  Config: config/agentproxy.yaml")
    print("  Logs:   logs/traffic.jsonl")
    print()

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def install() -> None:
    _banner("AgentProxy Installer")
    check_python()
    ensure_uv()
    sync_deps()
    generate_cert()
    trust_cert()
    install_wrappers()
    print_success()


def uninstall() -> None:
    _banner("AgentProxy Uninstaller")
    remove_wrappers()
    untrust_cert()
    print()
    print("  Uninstall complete.")
    print()
    print("  ~/.mitmproxy/ (cert files) were left in place.")
    print("  Remove manually if desired: rm -rf ~/.mitmproxy")
    print()


def main() -> None:
    args = [a.lower().lstrip("-") for a in sys.argv[1:]]
    if "uninstall" in args:
        uninstall()
    else:
        install()


if __name__ == "__main__":
    main()
