import io
import json
import tempfile
import unittest
from pathlib import Path

from air_ssh.application import Command, CycleWlan, OperationError, Request, UsageError, run
from air_ssh.application.run import execute


class FakeSession:
    def __init__(self, outcomes=None):
        self.calls = []
        self.outcomes = outcomes or {}

    def run(self, command):
        self.calls.append(command)
        outcome = self.outcomes.get(command, True)
        if isinstance(outcome, BaseException):
            raise outcome
        return outcome

    def save(self):
        self.calls.append("save config")

    def close(self):
        self.calls.append("close")


class ExecutionTests(unittest.TestCase):
    def test_cycles_restore_in_order_before_save(self):
        session = FakeSession()
        execute(
            Request(
                (CycleWlan("1"), Command("first"), CycleWlan("2"), Command("second")), save=True
            ),
            session,
            io.StringIO(),
        )
        self.assertEqual(
            session.calls,
            [
                "config wlan disable 1",
                "first",
                "config wlan enable 1",
                "config wlan disable 2",
                "second",
                "config wlan enable 2",
                "save config",
            ],
        )

    def test_exception_restores_current_cycle_only(self):
        for error in (OSError("lost channel"), KeyboardInterrupt()):
            session = FakeSession({"first": error})
            req = Request((CycleWlan("1"), Command("first"), CycleWlan("2")), save=True)
            with self.subTest(error=type(error)), self.assertRaises(type(error)):
                execute(req, session, io.StringIO())
            self.assertEqual(
                session.calls, ["config wlan disable 1", "first", "config wlan enable 1"]
            )

    def test_disable_failure_still_restores(self):
        session = FakeSession({"config wlan disable 1": False})
        with self.assertRaises(OperationError):
            execute(Request((CycleWlan("1"), Command("change"))), session, io.StringIO())
        self.assertEqual(session.calls, ["config wlan disable 1", "config wlan enable 1"])

    def test_cleanup_failure_does_not_mask_original_error(self):
        session = FakeSession(
            {"change": ValueError("original"), "config wlan enable 1": OSError("cleanup")}
        )
        err = io.StringIO()
        with self.assertRaisesRegex(ValueError, "original"):
            execute(Request((CycleWlan("1"), Command("change"))), session, err)
        self.assertIn("cleanup", err.getvalue())

    def test_failed_cleanup_prevents_save_and_next_cycle(self):
        session = FakeSession({"config wlan enable 1": False})
        with self.assertRaises(OperationError):
            execute(Request((CycleWlan("1"), CycleWlan("2")), save=True), session, io.StringIO())
        self.assertNotIn("config wlan disable 2", session.calls)
        self.assertNotIn("save config", session.calls)

    def test_timeout_continues_commands_but_reports_failure_and_skips_save(self):
        session = FakeSession({"first": False})
        with self.assertRaises(OperationError):
            execute(
                Request((Command("first"), Command("second")), save=True), session, io.StringIO()
            )
        self.assertEqual(session.calls, ["first", "second"])

    def test_inventory_to_session_and_disconnect_on_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "devices.json"
            path.write_text(
                json.dumps(
                    {
                        "devices": {
                            "lab": {
                                "host": "192.0.2.1",
                                "username": "operator",
                                "password": "test-secret",
                            }
                        }
                    }
                ),
                encoding="utf-8",
            )
            session = FakeSession({"show sysinfo": OSError("lost channel")})
            targets = []

            def connect(target, out, err):
                targets.append(target)
                return session

            with self.assertRaises(OSError):
                run(
                    Request((Command("show sysinfo"),), "lab", str(path)),
                    env={},
                    open_session=connect,
                )
            self.assertEqual(targets[0].password, "test-secret")
            self.assertEqual(session.calls, ["show sysinfo", "close"])

    def test_list_hides_password_without_connecting(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "devices.json"
            path.write_text(
                json.dumps(
                    {"lab": {"host": "192.0.2.1", "user": "operator", "password": "test-secret"}}
                ),
                encoding="utf-8",
            )
            out = io.StringIO()
            run(
                Request(inventory=str(path), list_devices=True),
                env={},
                out=out,
                open_session=lambda *args: self.fail("unexpected connection"),
            )
            self.assertIn("lab\t192.0.2.1\toperator", out.getvalue())
            self.assertNotIn("test-secret", out.getvalue())

    def test_missing_password_never_opens_session(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "devices.json"
            path.write_text('{"lab": {"host": "192.0.2.1", "user": "operator"}}', encoding="utf-8")
            with self.assertRaises(UsageError):
                run(
                    Request(save=True, device="lab", inventory=str(path)),
                    env={},
                    open_session=lambda *args: self.fail("unexpected connection"),
                )
