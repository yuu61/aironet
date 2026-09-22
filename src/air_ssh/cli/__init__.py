"""Argument parsing and exit status. Calls the application layer only."""

import argparse
import sys

from ..application import Command, CycleWlan, OperationError, Request, UsageError, run


def parse_args(argv: list[str]) -> Request:
    parser = argparse.ArgumentParser(
        prog="air-ssh",
        usage="%(prog)s [options] [--cycle-wlan ID] [COMMAND ...]",
        description="Cisco AireOS WLC / Mobility Express SSH helper.",
        epilog=(
            "--cycle-wlan ID (1..512) disables a WLAN before the following commands; "
            "the next cycle or end of the batch restores and verifies its original state. "
            "Errors stop the batch; restoration is attempted if the session is usable. "
            'Example: air-ssh -d wlc --cycle-wlan 1 "config wlan max-associated-clients 50 1"'
        ),
        allow_abbrev=False,
    )
    parser.add_argument("-d", "--device", help="device name in inventory ($AIRONET_DEVICE)")
    parser.add_argument("--inventory", help="inventory path ($AIRONET_INVENTORY)")
    parser.add_argument("--list", action="store_true", help="list devices without passwords")
    parser.add_argument("--save", action="store_true", help="save config after WLAN restoration")
    # Preserve command/cycle order while allowing global options anywhere.
    args, tokens = parser.parse_known_args(argv)
    operations = []
    iterator = iter(tokens)
    for token in iterator:
        if token == "--cycle-wlan":
            wlan_id = next(iterator, None)
            if wlan_id is None:
                raise UsageError("--cycle-wlan requires a WLAN id argument")
            operations.append(CycleWlan(wlan_id))
        elif token.startswith("-"):
            raise UsageError(f"unknown option: {token}")
        else:
            operations.append(Command(token))
    if args.list and (operations or args.save):
        raise UsageError("--list cannot be combined with commands or --save")
    return Request(tuple(operations), args.device, args.inventory, args.save, args.list)


def main(argv: list[str] | None = None) -> int:
    try:
        run(parse_args(sys.argv[1:] if argv is None else argv))
    except (UsageError, OperationError, OSError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("ERROR: interrupted", file=sys.stderr)
        return 130
    return 0
