"""Netmiko transport and AireOS's streaming/prompt protocol."""

import re
import sys
import time
from typing import TextIO

from ..domain import OperationError, Target, UsageError

CONFIRM_RE = re.compile(
    r"(?:Are you sure\b|Would you like\b|Do you (?:want|wish)\b|Proceed\b|Please confirm\b)"
    r"[^\r\n]*\(y/n\)\s*[:?]?",
    re.IGNORECASE,
)
SAVE_CONFIRM_RE = re.compile(r"Are you sure you want to save\?\s*\(y/n\)\s*[:?]?", re.IGNORECASE)
ENTER_RE = re.compile(
    r"Press (?:Enter|any key) to continue(?:\s+Or <Ctl Z> to abort)?\.*", re.IGNORECASE
)
MORE_RE = re.compile(r"--More--(?:\s+or \(q\)uit)?", re.IGNORECASE)
PROMPT_RE = re.compile(r"\([^\r\n]+\)\s*>")
ERROR_RE = re.compile(
    r"^\s*(?:%\s*)?(?:Request failed\b|Error(?:\s*:|!|$)|"
    r"Invalid (?:command|input|parameter|argument|WLAN)\b|"
    r"Incorrect usage\b|Usage\s*:|Command (?:failed|not found)\b|"
    r"Unable to\b|Permission denied\b|Not authorized\b)",
    re.IGNORECASE | re.MULTILINE,
)
ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
DEFAULT_TIMEOUT = 120


class NetmikoSession:
    def __init__(self, conn, out: TextIO, err: TextIO):
        self._conn = conn
        self._out = out
        self._err = err
        self._ready = True
        base_prompt = getattr(conn, "base_prompt", None)
        self._prompt = (
            re.compile(re.escape(base_prompt.strip()) + r"\s*>")
            if isinstance(base_prompt, str) and base_prompt.strip()
            else PROMPT_RE
        )

    def run(self, command: str, timeout: int = DEFAULT_TIMEOUT) -> bool:
        """Stream a command. Timeout is inactivity, not total runtime."""
        return self._exchange(command, timeout) is not None

    def _exchange(self, command: str, timeout: int = DEFAULT_TIMEOUT) -> str | None:
        if not self._ready:
            raise OperationError("SSH command state is unknown; reconnect before further commands")
        print(f"===== {command} =====", file=self._out)
        self._ready = False
        self._conn.write_channel(command + "\n")
        tail = ""
        chunks = []
        prompt_pending = False
        last_data = time.monotonic()
        while True:
            chunk = self._conn.read_channel()
            if chunk:
                print(chunk, end="", flush=True, file=self._out)
                chunks.append(chunk)
                tail = (tail + chunk)[-4096:]
                last_data = time.monotonic()
                prompt_pending = False
                continue
            # Only answer a complete waiting line, never a substring in normal output
            # or a command echo. A quiet read lets fragmented lines finish first.
            clean_tail = ANSI_RE.sub("", tail).replace("\r", "\n").rstrip()
            line = clean_tail.rsplit("\n", 1)[-1].strip()
            confirm = SAVE_CONFIRM_RE if command.strip().lower() == "save config" else CONFIRM_RE
            response = None
            is_echo = line == command.strip()
            if not is_echo and ENTER_RE.fullmatch(line):
                response = "\n"
            elif not is_echo and MORE_RE.fullmatch(line):
                response = " "
            elif not is_echo and confirm.fullmatch(line):
                response = "y\n"
            if response is not None:
                self._conn.write_channel(response)
                tail = ""
                prompt_pending = False
            elif self._prompt.fullmatch(line):
                # The prompt also precedes command echo: wait for two quiet reads.
                if prompt_pending:
                    self._ready = True
                    print(file=self._out)
                    output = ANSI_RE.sub("", "".join(chunks)).replace("\r", "\n")
                    # A bare command echo must not be mistaken for an error response.
                    output = "\n".join(
                        row for row in output.splitlines() if row.strip() != command.strip()
                    )
                    if ERROR_RE.search(output):
                        raise OperationError(f"controller rejected '{command}'")
                    return output
                prompt_pending = True
            elif time.monotonic() - last_data > timeout:
                print(f"\n[WARN] no output for {timeout}s on '{command}'", file=self._err)
                return None
            time.sleep(0.3)

    def save(self) -> None:
        output = self._exchange("save config")
        if output is None or not re.search(
            r"^\s*Configuration Saved!\s*$", output, re.MULTILINE | re.IGNORECASE
        ):
            raise OperationError("configuration save was not confirmed")

    def wlan_enabled(self, wlan_id: str) -> bool:
        output = self._exchange(f"show wlan {wlan_id}")
        if output is not None:
            identifiers = re.findall(r"^WLAN Identifier\.*\s+(\d+)\s*$", output, re.MULTILINE)
            states = re.findall(
                r"^Status\.*\s+(Enabled|Disabled)\s*$", output, re.MULTILINE | re.IGNORECASE
            )
            if identifiers == [wlan_id] and len(states) == 1:
                return states[0].lower() == "enabled"
        raise OperationError(f"could not determine status of WLAN {wlan_id}")

    def close(self) -> None:
        if not self._ready:
            # Close the transport first so Netmiko's logout/paging cleanup cannot
            # send CLI commands into a pending confirmation or unfinished command.
            self._conn.paramiko_cleanup()
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
    session = NetmikoSession(conn, out, err)
    try:
        # Netmiko disables paging during login. Cisco warns that large unpaged
        # output can terminate the session; handle MORE explicitly instead.
        if not session.run("config paging enable"):
            raise OperationError("could not enable CLI paging")
    except BaseException:
        try:
            session.close()
        except Exception as exc:  # noqa: BLE001 -- preserve initialization failure
            print(f"[WARN] disconnect after initialization failure failed: {exc}", file=err)
        raise
    return session
