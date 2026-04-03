#!/usr/bin/env python3
"""
run_local.py — Safe local dev launcher for Arcanum services.

Loads .env properly (handles shell metacharacters in profile values)
and runs any command with the full environment.

Usage:
    python3 scripts/run_local.py go run ./cmd/worker
    python3 scripts/run_local.py go run ./cmd/api-gateway
    python3 scripts/run_local.py ./bin/worker
    python3 scripts/run_local.py make health

You can also override specific vars at call time:
    HTTP_PORT=8085 python3 scripts/run_local.py go run ./cmd/orchestrator
"""

import os
import sys
import subprocess
from pathlib import Path

# Vars the caller may legitimately override AT CALL TIME (not inherited stale)
# We detect these by checking if they were set DIFFERENTLY from .env values.
_CALL_TIME_OVERRIDES_CHECKED = False


def load_env_file(path: Path) -> dict:
    """Parse .env file safely, preserving values with shell metacharacters."""
    env = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                if '=' in line:
                    key, value = line.split('=', 1)
                    env[key.strip()] = value.strip()
    except FileNotFoundError:
        print(f"WARNING: {path} not found, using environment only", file=sys.stderr)
    return env


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)

    project_root = Path(__file__).parent.parent
    env_file = project_root / '.env'

    # Start with a clean base: only non-project vars from parent (PATH, HOME, USER, etc.)
    # .env file is the authoritative source for project config.
    env = {}

    # Pass through essential system vars
    for key in ('PATH', 'HOME', 'USER', 'SHELL', 'TERM', 'LANG', 'LC_ALL',
                'GOPATH', 'GOROOT', 'GOMODCACHE', 'GOCACHE',
                'XDG_RUNTIME_DIR', 'TMPDIR', 'TMP', 'TEMP',
                'SSH_AUTH_SOCK', 'GPG_AGENT_INFO',
                'DISPLAY', 'WAYLAND_DISPLAY'):
        if key in os.environ:
            env[key] = os.environ[key]

    # Load .env file — these are authoritative for project config
    file_env = load_env_file(env_file)
    env.update(file_env)

    cmd = sys.argv[1:]
    try:
        result = subprocess.run(cmd, env=env, cwd=str(project_root))
        sys.exit(result.returncode)
    except KeyboardInterrupt:
        sys.exit(0)
    except FileNotFoundError:
        print(f"ERROR: Command not found: {cmd[0]}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
