from collections.abc import Mapping
from dataclasses import dataclass, field

from .credentials import resolve_enable_password, resolve_password
from .errors import UsageError
from .inventory import entry_text

# "wlc": AireOS WLC / Mobility Express controller CLI ("me" is accepted as an alias).
# "ap": a Wave 2 / Catalyst Wi-Fi 6 AP's own CLI (user EXEC ">" then privileged EXEC "#").
KINDS = {"wlc": "wlc", "me": "wlc", "ap": "ap"}


@dataclass(frozen=True)
class Target:
    name: str
    host: str
    username: str
    password: str = field(repr=False)
    port: int = 22
    kind: str = "wlc"
    enable_password: str | None = field(default=None, repr=False)

    @property
    def is_ap(self) -> bool:
        return self.kind == "ap"


def resolve_kind(entry: Mapping, name: str) -> str:
    raw = entry_text(entry, "kind") or "wlc"
    kind = KINDS.get(raw.strip().lower())
    if kind is None:
        raise UsageError(f"unknown kind {raw!r} for {name!r}; use wlc, me, or ap")
    return kind


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
    kind = resolve_kind(entry, name)
    password = resolve_password(entry, env)
    enable = resolve_enable_password(entry, env, password) if kind == "ap" else None
    return Target(name, host, username, password, port, kind, enable)
