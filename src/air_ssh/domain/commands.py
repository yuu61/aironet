"""Ordered commands and WLAN cycles, independent of SSH and argument parsing."""

from dataclasses import dataclass

from .errors import UsageError

# Root-level words that open a sub-mode prompt on their own instead of running
# anything. air-ssh only recognizes the root prompt, so such a token would wait
# for the inactivity timeout.
MODE_WORDS = frozenset({"clear", "config", "debug", "reset", "save", "show", "transfer"})
# Commands that end the CLI session; air-ssh disconnects by itself.
SESSION_WORDS = frozenset({"exit", "logout"})


@dataclass(frozen=True)
class Command:
    text: str

    def __post_init__(self):
        if not self.text.strip() or any(c in self.text for c in "\r\n\x00"):
            raise UsageError("commands must be non-empty single lines")
        words = self.text.lower().split()
        shown = self.text.strip()
        if len(words) == 1 and words[0] in MODE_WORDS:
            raise UsageError(
                f"'{shown}' alone only opens a sub-mode prompt; give the complete command"
            )
        if words[0] in SESSION_WORDS:
            raise UsageError(f"'{shown}' ends the session; air-ssh disconnects by itself")
        if words[:2] == ["config", "prompt"]:
            raise UsageError(
                "config prompt changes the prompt air-ssh waits for; run it from another session"
            )


@dataclass(frozen=True)
class CycleWlan:
    wlan_id: str

    def __post_init__(self):
        normalized = self.wlan_id.lstrip("0")
        if (
            not normalized.isascii()
            or not normalized.isdecimal()
            or len(normalized) > 3
            or not 1 <= int(normalized) <= 512
        ):
            raise UsageError("--cycle-wlan requires a WLAN id in 1..512")
        object.__setattr__(self, "wlan_id", normalized)

    @property
    def disable(self) -> str:
        return f"config wlan disable {self.wlan_id}"

    @property
    def enable(self) -> str:
        return f"config wlan enable {self.wlan_id}"
