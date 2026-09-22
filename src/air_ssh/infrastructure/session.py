"""Netmiko transport and AireOS's streaming/prompt protocol."""

import re
import sys
import time
from typing import TextIO

from ..domain import OperationError, Target, UsageError

# A waiting question ends its line with (y/n); the controller may put a warning
# sentence before it on the same line ("Clear ap-config will ... reboot the AP.
# Are you sure you want continue? (y/n)"). Dotted leaders mark show output, never
# a question, so a line containing them is never answered.
CONFIRM_RE = re.compile(
    r"(?![^\r\n]*\.{3,})[^\r\n]*?"
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
# A documented refusal of a config command ("Cannot change Exp Bw Req mode while
# 802.11a network is operational."). Only config commands are judged by it, so a
# show dump that happens to start a line with the word cannot fail a status read.
CONFIG_ERROR_RE = re.compile(r"^\s*Cannot\b", re.IGNORECASE | re.MULTILINE)
# A Wave 2 / Catalyst Wi-Fi 6 AP: user EXEC "hostname>" or privileged EXEC "hostname#",
# and the CLI error messages the AP command reference documents.
AP_PROMPT_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*[>#]")
AP_ERROR_RE = re.compile(
    r"^\s*%\s*(?:Ambiguous command\b|Incomplete command\b|Invalid input\b)",
    re.IGNORECASE | re.MULTILINE,
)
ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
DEFAULT_TIMEOUT = 120
# How long the escape key may take to bring the root prompt back after an abandoned command.
RECOVERY_TIMEOUT = 10
# q exits MORE output; Ctrl-Z returns to the root prompt from any mode and aborts
# a "Press Enter to continue Or <Ctl Z> to abort" pause.
QUIT_MORE = "q"
CTRL_Z = "\x1a"


def waiting_line(tail: str) -> str:
    """The last line the controller has printed, as a human would see it."""
    clean_tail = ANSI_RE.sub("", tail).replace("\r", "\n").rstrip()
    return clean_tail.rsplit("\n", 1)[-1].strip()


class NetmikoSession:
    """AireOS WLC / Mobility Express controller CLI."""

    DEVICE = "controller"
    # What follows Netmiko's base prompt, and the fallback when there is none.
    PROMPT_TAIL = r"\s*>"
    FALLBACK_PROMPT = PROMPT_RE

    def __init__(self, conn, out: TextIO, err: TextIO):
        self._conn = conn
        self._out = out
        self._err = err
        self._ready = True
        base_prompt = getattr(conn, "base_prompt", None)
        self._prompt = (
            re.compile(re.escape(base_prompt.strip()) + self.PROMPT_TAIL)
            if isinstance(base_prompt, str) and base_prompt.strip()
            else self.FALLBACK_PROMPT
        )

    def run(self, command: str, timeout: int = DEFAULT_TIMEOUT) -> bool:
        """Stream a command. Timeout is inactivity, not total runtime."""
        return self._exchange(command, timeout) is not None

    def _rejected(self, command: str, output: str) -> bool:
        is_config = command.split()[0].lower() == "config"
        return bool(ERROR_RE.search(output) or (is_config and CONFIG_ERROR_RE.search(output)))

    def _answer(self, command: str, line: str) -> str | None:
        """What to type at a complete waiting line, or None to keep waiting."""
        if ENTER_RE.fullmatch(line):
            return "\n"
        if MORE_RE.fullmatch(line):
            return " "
        confirm = SAVE_CONFIRM_RE if command.strip().lower() == "save config" else CONFIRM_RE
        if confirm.fullmatch(line):
            return "y\n"
        return None

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
            line = waiting_line(tail)
            response = None if line == command.strip() else self._answer(command, line)
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
                    if self._rejected(command, output):
                        raise OperationError(f"{self.DEVICE} rejected '{command}'")
                    return output
                prompt_pending = True
            elif time.monotonic() - last_data > timeout:
                print(f"\n[WARN] no output for {timeout}s on '{command}'", file=self._err)
                self._recover(command, line)
                return None
            time.sleep(0.3)

    def _recover(self, command: str, line: str) -> None:
        """Abandon the pending command and try to get the root prompt back.

        A MORE pause that MORE_RE did not recognize (debug output can be appended
        to it) is left with q; anything else with Ctrl-Z. Regaining the prompt lets
        a WLAN disabled earlier in the batch be restored. The abandoned command
        stays a failure either way.
        """
        self._conn.write_channel(QUIT_MORE if "--more--" in line.lower() else CTRL_Z)
        tail = ""
        prompt_pending = False
        last_data = time.monotonic()
        while True:
            chunk = self._conn.read_channel()
            if chunk:
                print(chunk, end="", flush=True, file=self._out)
                tail = (tail + chunk)[-4096:]
                last_data = time.monotonic()
                prompt_pending = False
                continue
            if self._prompt.fullmatch(waiting_line(tail)):
                if prompt_pending:
                    self._ready = True
                    print(file=self._out)
                    print(f"[WARN] prompt recovered; '{command}' was abandoned", file=self._err)
                    return
                prompt_pending = True
            elif time.monotonic() - last_data > RECOVERY_TIMEOUT:
                print(
                    "[WARN] prompt not recovered; reconnect before further commands", file=self._err
                )
                return
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


class ApSession(NetmikoSession):
    """A Wave 2 / Catalyst Wi-Fi 6 AP's own CLI, in privileged EXEC.

    Netmiko's IOS handling has already set "terminal length 0", so nothing pauses,
    and the AP command reference documents no questions to answer: an unexpected
    prompt is left to the inactivity timeout and Ctrl-Z. WLAN cycles and
    "save config" belong to the controller CLI and are refused.
    """

    DEVICE = "AP"
    PROMPT_TAIL = r"[>#]"
    FALLBACK_PROMPT = AP_PROMPT_RE

    def _rejected(self, command: str, output: str) -> bool:
        return AP_ERROR_RE.search(output) is not None

    def _answer(self, command: str, line: str) -> str | None:
        return None

    def save(self) -> None:
        raise UsageError("--save applies to controllers; the AP CLI has no save config")

    def wlan_enabled(self, wlan_id: str) -> bool:
        raise UsageError("--cycle-wlan applies to controllers; the AP CLI has no WLANs")


def open_session(target: Target, out: TextIO, err: TextIO) -> NetmikoSession:
    # Help and inventory listing do not require Netmiko.
    try:
        from netmiko import ConnectHandler
        from netmiko.exceptions import NetmikoAuthenticationException, NetmikoTimeoutException
    except ImportError:
        raise UsageError(f"netmiko is not installed for {sys.executable}; run uv sync") from None
    options = {
        "host": target.host,
        "port": target.port,
        "username": target.username,
        "password": target.password,
        "fast_cli": False,
    }
    if target.is_ap:
        options |= {"device_type": "cisco_ios", "secret": target.enable_password}
    else:
        options["device_type"] = "cisco_wlc_ssh"
    try:
        conn = ConnectHandler(**options)
    except (NetmikoAuthenticationException, NetmikoTimeoutException, OSError) as exc:
        raise OperationError(
            f"SSH connection to {target.name!r} failed ({type(exc).__name__})"
        ) from None
    session = ApSession(conn, out, err) if target.is_ap else NetmikoSession(conn, out, err)
    try:
        if target.is_ap:
            _enter_privileged_exec(conn, target, NetmikoTimeoutException)
        else:
            _enable_paging(session, err)
    except BaseException:
        try:
            session.close()
        except Exception as exc:  # noqa: BLE001 -- preserve initialization failure
            print(f"[WARN] disconnect after initialization failure failed: {exc}", file=err)
        raise
    return session


def _enter_privileged_exec(conn, target: Target, timeout_error: type) -> None:
    # The AP starts in user EXEC (">"); "enable" asks for the secret. Netmiko reports
    # a wrong or missing secret as ValueError, which must not leak past the CLI.
    try:
        conn.enable()
    except (ValueError, timeout_error, OSError):
        raise OperationError(
            f"could not enter privileged EXEC on {target.name!r}; check enable_password"
        ) from None


def _enable_paging(session: NetmikoSession, err: TextIO) -> None:
    # Netmiko disables paging during login. Cisco warns that large unpaged
    # output can terminate the session; handle MORE explicitly instead.
    try:
        enabled = session.run("config paging enable")
    except OperationError:
        # config paging needs read-write privileges. For a read-only user Netmiko's
        # "config paging disable" was refused the same way, so paging is still on
        # and MORE handling suffices; the warning covers the remaining case.
        print(
            "[WARN] controller refused 'config paging enable' (read-write privileges "
            "required); continuing. If this account is read-write, paging is off and "
            "a very long output may end the session.",
            file=err,
        )
        return
    if not enabled:
        raise OperationError("could not enable CLI paging")
