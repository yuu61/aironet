import io
import unittest
from contextlib import redirect_stderr
from unittest.mock import patch

from air_ssh.application import Command, CycleWlan, OperationError, UsageError
from air_ssh.cli import main, parse_args


class CliTests(unittest.TestCase):
    def test_interleaved_cycles_commands_and_global_flags(self):
        req = parse_args(
            [
                "--cycle-wlan",
                "1",
                "first",
                "--device",
                "lab",
                "--save",
                "--cycle-wlan",
                "2",
                "second",
                "--inventory",
                "local.json",
            ]
        )
        self.assertEqual(
            req.operations, (CycleWlan("1"), Command("first"), CycleWlan("2"), Command("second"))
        )
        self.assertEqual(req.device, "lab")
        self.assertEqual(req.inventory, "local.json")
        self.assertTrue(req.save)

    def test_invalid_cli_requests(self):
        for args in (
            ["--cycle-wlan"],
            ["--cycle-wlan", "--save"],
            ["--typo"],
            ["--list", "show sysinfo"],
            ["--list", "--save"],
            ["show sysinfo\nsave config"],
        ):
            with self.subTest(args=args), self.assertRaises(UsageError):
                parse_args(args)

    def test_error_exit_status(self):
        with (
            patch("air_ssh.cli.run", side_effect=OperationError("failed")),
            redirect_stderr(io.StringIO()) as err,
        ):
            self.assertEqual(main(["--save"]), 1)
        self.assertIn("ERROR: failed", err.getvalue())

    def test_no_commands_exit_without_connecting(self):
        with redirect_stderr(io.StringIO()) as err:
            self.assertEqual(main([]), 1)
        self.assertIn("no commands", err.getvalue())
