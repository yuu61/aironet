"""Ordered commands and WLAN cycles, independent of SSH and argument parsing."""

from dataclasses import dataclass

from .errors import UsageError


@dataclass(frozen=True)
class Command:
    text: str

    def __post_init__(self):
        if not self.text.strip() or any(c in self.text for c in "\r\n\x00"):
            raise UsageError("commands must be non-empty single lines")


@dataclass(frozen=True)
class CycleWlan:
    wlan_id: str

    def __post_init__(self):
        if not self.wlan_id.isascii() or not self.wlan_id.isdecimal() or int(self.wlan_id) < 1:
            raise UsageError("--cycle-wlan requires a positive WLAN id")

    @property
    def disable(self) -> str:
        return f"config wlan disable {self.wlan_id}"

    @property
    def enable(self) -> str:
        return f"config wlan enable {self.wlan_id}"
