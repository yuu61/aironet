from collections.abc import Mapping
from dataclasses import dataclass, field

from .credentials import resolve_password
from .errors import UsageError
from .inventory import entry_text


@dataclass(frozen=True)
class Target:
    name: str
    host: str
    username: str
    password: str = field(repr=False)
    port: int = 22


def resolve_target(name: str, entry: Mapping, env: Mapping[str, str]) -> Target:
    host = entry_text(entry, "host", "hostname", "address", "ip")
    username = entry_text(entry, "username", "user")
    if not host or not host.strip():
        raise UsageError(f"no host for {name!r}; add host to devices.json")
    if not username or not username.strip():
        raise UsageError(f"no username for {name!r}; add username to devices.json")
    port = entry.get("port", 22)
    if isinstance(port, str) and port.isascii() and port.isdecimal():
        port = int(port)
    if type(port) is not int or not 1 <= port <= 65535:
        raise UsageError(f"invalid SSH port for {name!r}; expected 1..65535")
    return Target(name, host, username, resolve_password(entry, env), port)
