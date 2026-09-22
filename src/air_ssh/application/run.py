"""One invocation: select the target, execute operations, restore WLANs, close."""

import os
import sys
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Protocol, TextIO

from .. import infrastructure
from ..domain import (
    Command,
    CycleWlan,
    OperationError,
    Target,
    UsageError,
    entry_text,
    resolve_target,
    select_entry,
)


@dataclass(frozen=True)
class Request:
    operations: tuple[Command | CycleWlan, ...] = ()
    device: str | None = None
    inventory: str | None = None
    save: bool = False
    list_devices: bool = False


class Session(Protocol):
    def run(self, command: str) -> bool: ...
    def wlan_enabled(self, wlan_id: str) -> bool: ...
    def save(self) -> None: ...
    def close(self) -> None: ...


def restore_wlan(session: Session, cycle: CycleWlan, was_enabled: bool) -> None:
    if session.wlan_enabled(cycle.wlan_id) != was_enabled:
        command = cycle.enable if was_enabled else cycle.disable
        if not session.run(command):
            raise OperationError(f"WLAN {cycle.wlan_id} restoration timed out")
        if session.wlan_enabled(cycle.wlan_id) != was_enabled:
            raise OperationError(f"WLAN {cycle.wlan_id} did not return to its original state")


def execute(req: Request, session: Session, err: TextIO) -> None:
    active: tuple[CycleWlan, bool] | None = None
    failed = False
    try:
        for operation in req.operations:
            if isinstance(operation, CycleWlan):
                if active is not None:
                    restore_wlan(session, *active)
                    active = None
                was_enabled = session.wlan_enabled(operation.wlan_id)
                # Register before sending: a failed read may follow a successful disable.
                active = (operation, was_enabled)
                if was_enabled and not session.run(operation.disable):
                    raise OperationError("WLAN disable timed out; stopping the cycle")
                if was_enabled and session.wlan_enabled(operation.wlan_id):
                    raise OperationError("WLAN is still enabled; stopping the cycle")
            elif not session.run(operation.text):
                raise OperationError("command timed out; stopping the batch without saving")
    finally:
        if active is not None:
            try:
                restore_wlan(session, *active)
            except Exception as exc:  # noqa: BLE001 -- preserve the original operation's error
                failed = True
                print(f"[ERROR] cleanup WLAN {active[0].wlan_id} failed: {exc}", file=err)
    if failed:
        raise OperationError("one or more commands failed; configuration was not saved")
    # Persist the restored WLAN state, rather than the temporary disabled state.
    if req.save:
        session.save()


def run(
    req: Request,
    env: Mapping[str, str] | None = None,
    out: TextIO | None = None,
    err: TextIO | None = None,
    open_session: Callable[[Target, TextIO, TextIO], Session] = infrastructure.open_session,
) -> None:
    env = os.environ if env is None else env
    out = sys.stdout if out is None else out
    err = sys.stderr if err is None else err
    if not req.list_devices and not req.operations and not req.save:
        raise UsageError("no commands provided (give commands, --save, or --list)")
    devices, path = infrastructure.read_inventory(req.inventory, env)
    if req.list_devices:
        print(f"Inventory: {path}", file=out)
        for name, entry in devices.items():
            host = entry_text(entry, "host", "hostname", "address", "ip") or "(no host)"
            user = entry_text(entry, "username", "user") or "(no username)"
            print(f"{name}\t{host}\t{user}", file=out)
        if not devices:
            print("No devices. Add devices to the inventory shown above.", file=out)
        return
    name, entry = select_entry(req.device, devices, env)
    target = resolve_target(name, entry, env)
    session = open_session(target, out, err)
    try:
        execute(req, session, err)
    finally:
        try:
            session.close()
        except Exception as exc:  # noqa: BLE001 -- disconnect must not mask an operation failure
            # Netmiko's paging reset can fail on an already-busy channel.
            print(f"[WARN] disconnect failed: {exc}", file=err)
