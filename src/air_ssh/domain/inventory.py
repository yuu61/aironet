"""Inventory structure and device selection. No filesystem access."""

from collections.abc import Mapping

from .errors import UsageError


def parse_inventory(data: object, where: str) -> dict[str, dict]:
    devices = data.get("devices", data) if isinstance(data, Mapping) else None
    if not isinstance(devices, Mapping):
        raise UsageError(f'{where}: expected {{"devices": {{name: {{...}}}}}}')
    result = {}
    for name, entry in devices.items():
        if not isinstance(name, str) or not name:
            raise UsageError(f"{where}: device names must be non-empty strings")
        if name.startswith("_"):
            continue
        if not isinstance(entry, Mapping):
            raise UsageError(f"{where}: device {name!r} must be an object")
        result[name] = dict(entry)
    return result


def entry_text(entry: Mapping, *keys: str) -> str | None:
    for key in keys:
        value = entry.get(key)
        if value is None or value == "":
            continue
        if not isinstance(value, str):
            raise UsageError(f"inventory field {key!r} must be a string")
        return value
    return None


def select_entry(
    device: str | None, devices: Mapping[str, Mapping], env: Mapping[str, str]
) -> tuple[str, Mapping]:
    name = device or env.get("AIRONET_DEVICE")
    if not name:
        raise UsageError("select a device with --device NAME or $AIRONET_DEVICE (see --list)")
    if name not in devices:
        raise UsageError(f"unknown device {name!r} (see --list)")
    return name, devices[name]
