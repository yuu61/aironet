"""Locate and decode the agent-independent inventory."""

import json
import os
from collections.abc import Mapping
from pathlib import Path

from ..domain import UsageError, parse_inventory


def inventory_path(override: str | None, env: Mapping[str, str]) -> Path:
    chosen = override or env.get("AIRONET_INVENTORY")
    return Path(chosen).expanduser() if chosen else Path.home() / ".aironet" / "devices.json"


def read_inventory(
    override: str | None = None, env: Mapping[str, str] | None = None
) -> tuple[dict[str, dict], Path]:
    env = os.environ if env is None else env
    path = inventory_path(override, env)
    try:
        data = json.loads(path.read_text(encoding="utf-8-sig"))
    except FileNotFoundError:
        # A first --list should explain where to create the inventory.
        if not override and not env.get("AIRONET_INVENTORY"):
            return {}, path
        raise UsageError(f"inventory file not found: {path}") from None
    except json.JSONDecodeError as exc:
        raise UsageError(
            f"invalid JSON in {path} at line {exc.lineno}, column {exc.colno}"
        ) from None
    except (OSError, UnicodeError):
        # Do not include file contents or a decoder's offending bytes (passwords).
        raise UsageError(f"cannot read inventory: {path}") from None
    return parse_inventory(data, str(path)), path
