#!/usr/bin/env python3
"""Compatibility entry point; prefer the installed air-ssh command."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from air_ssh.cli import main

if __name__ == "__main__":
    sys.exit(main())
