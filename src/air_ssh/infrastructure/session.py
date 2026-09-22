"""Netmiko transport and AireOS's streaming/prompt protocol."""

import re
import sys
import time
from typing import TextIO

from ..domain import OperationError, Target, UsageError

CONFIRM_PATTERNS = re.compile(r"\(y/n\)|are you sure|confirm", re.IGNORECASE)
ENTER_PATTERNS = re.compile(r"press (enter|any key) to continue", re.IGNORECASE)
PROMPT_RE = re.compile(r"\(.+\) >\s*$")
DEFAULT_TIMEOUT = 120


class NetmikoSession:
    def __init__(self, conn, out: TextIO, err: TextIO):
        self._conn = conn
        self._out = out
        self._err = err

    def run(self, command: str, timeout: int = DEFAULT_TIMEOUT) -> bool:
        """Stream a command. Timeout is inactivity, not total runtime."""
        print(f"===== {command} =====", file=self._out)
        self._conn.write_channel(command + "\n")
        tail = ""
        prompt_pending = False
        last_data = time.monotonic()
        while True:
            chunk = self._conn.read_channel()
            if chunk:
                print(chunk, end="", flush=True, file=self._out)
                tail = (tail + chunk)[-120:]
                last_data = time.monotonic()
                prompt_pending = False
                if ENTER_PATTERNS.search(tail):
                    self._conn.write_channel("\n")
                    tail = ""
                elif CONFIRM_PATTERNS.search(tail):
                    self._conn.write_channel("y\n")
                    tail = ""
                continue
            if PROMPT_RE.search(tail):
                # The prompt also precedes command echo: wait for two quiet reads.
                if prompt_pending:
                    print(file=self._out)
                    return True
                prompt_pending = True
            elif time.monotonic() - last_data > timeout:
                print(f"\n[WARN] no output for {timeout}s on '{command}'", file=self._err)
                return False
            time.sleep(0.3)

    def save(self) -> None:
        print("===== save config =====", file=self._out)
        self._conn.write_channel("save config\n")
        time.sleep(2)
        self._conn.write_channel("y\n")
        time.sleep(5)
        print(self._conn.read_channel(), file=self._out)

    def close(self) -> None:
        self._conn.disconnect()


def open_session(target: Target, out: TextIO, err: TextIO) -> NetmikoSession:
    # Help and inventory listing do not require Netmiko.
    try:
        from netmiko import ConnectHandler
        from netmiko.exceptions import NetmikoAuthenticationException, NetmikoTimeoutException
    except ImportError:
        raise UsageError(f"netmiko is not installed for {sys.executable}; run uv sync") from None
    try:
        conn = ConnectHandler(
            device_type="cisco_wlc_ssh",
            host=target.host,
            port=target.port,
            username=target.username,
            password=target.password,
            fast_cli=False,
        )
    except (NetmikoAuthenticationException, NetmikoTimeoutException, OSError) as exc:
        raise OperationError(
            f"SSH connection to {target.name!r} failed ({type(exc).__name__})"
        ) from None
    return NetmikoSession(conn, out, err)
