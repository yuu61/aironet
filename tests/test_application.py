import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from air_ssh.application import Command, CycleWlan, OperationError, Request, UsageError, run
from air_ssh.application.run import execute
from air_ssh.infrastructure.session import NetmikoSession


class ScriptedChannel:
    """Release replies only after their expected command or confirmation is sent."""

    base_prompt = "(Lab Controller)"

    def __init__(self, test, exchanges):
        self.test = test
        self.exchanges = list(exchanges)
        self.pending = ""
        self.writes = []

    def write_channel(self, command):
        self.test.assertTrue(self.exchanges, f"unexpected command: {command}")
        expected, self.pending = self.exchanges.pop(0)
        self.test.assertEqual(command, expected)
        self.writes.append(command)

    def read_channel(self):
        result, self.pending = self.pending, ""
        return result


class FakeSession:
    def __init__(self, outcomes=None, states=None, unchanged=()):
        self.calls = []
        self.outcomes = outcomes or {}
        self.states = dict(states or {})
        self.unchanged = unchanged

    def wlan_enabled(self, wlan_id):
        self.calls.append(f"show wlan {wlan_id}")
        outcome = self.outcomes.get(f"show wlan {wlan_id}", self.states.get(wlan_id, True))
        if isinstance(outcome, BaseException):
            raise outcome
        return outcome

    def run(self, command):
        self.calls.append(command)
        outcome = self.outcomes.get(command, True)
        if isinstance(outcome, BaseException):
            raise outcome
        if outcome and command not in self.unchanged:
            for action in ("enable", "disable"):
                if command.startswith(f"config wlan {action} "):
                    self.states[command.split()[-1]] = action == "enable"
        return outcome

    def save(self):
        self.calls.append("save config")

    def close(self):
        self.calls.append("close")


class ExecutionTests(unittest.TestCase):
    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_documented_restore_error_prevents_save_through_real_session(self, sleep):
        prompt = "\n(Lab Controller) >"
        status = "WLAN Identifier........ 1\nStatus........ "
        channel = ScriptedChannel(
            self,
            [
                ("show wlan 1\n", status + "Enabled" + prompt),
                ("config wlan disable 1\n", prompt),
                ("show wlan 1\n", status + "Disabled" + prompt),
                ("change\n", prompt),
                ("show wlan 1\n", status + "Disabled" + prompt),
                (
                    "config wlan enable 1\n",
                    "Request failed for wlan 1 - invalid security settings" + prompt,
                ),
            ],
        )
        err = io.StringIO()
        with self.assertRaises(OperationError):
            execute(
                Request((CycleWlan("1"), Command("change")), save=True),
                NetmikoSession(channel, io.StringIO(), err),
                err,
            )
        self.assertEqual(channel.exchanges, [])
        self.assertNotIn("save config\n", channel.writes)
        self.assertIn("cleanup WLAN 1 failed", err.getvalue())

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_disabled_wlan_and_confirmed_save_through_real_session(self, sleep):
        prompt = "\n(Lab Controller) >"
        status = "WLAN Identifier........ 1\nStatus........ Disabled" + prompt
        channel = ScriptedChannel(
            self,
            [
                ("show wlan 1\n", status),
                ("change\n", prompt),
                ("show wlan 1\n", status),
                ("save config\n", "Are you sure you want to save? (y/n)"),
                ("y\n", "y\nConfiguration Saved!" + prompt),
            ],
        )
        execute(
            Request((CycleWlan("1"), Command("change")), save=True),
            NetmikoSession(channel, io.StringIO(), io.StringIO()),
            io.StringIO(),
        )
        self.assertEqual(channel.exchanges, [])
        self.assertNotIn("config wlan enable 1\n", channel.writes)

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
                "show wlan 1",
                "config wlan disable 1",
                "show wlan 1",
                "first",
                "show wlan 1",
                "config wlan enable 1",
                "show wlan 1",
                "show wlan 2",
                "config wlan disable 2",
                "show wlan 2",
                "second",
                "show wlan 2",
                "config wlan enable 2",
                "show wlan 2",
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
                session.calls,
                [
                    "show wlan 1",
                    "config wlan disable 1",
                    "show wlan 1",
                    "first",
                    "show wlan 1",
                    "config wlan enable 1",
                    "show wlan 1",
                ],
            )

    def test_disable_failure_still_restores(self):
        session = FakeSession({"config wlan disable 1": False})
        with self.assertRaises(OperationError):
            execute(Request((CycleWlan("1"), Command("change"))), session, io.StringIO())
        self.assertEqual(session.calls, ["show wlan 1", "config wlan disable 1", "show wlan 1"])

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

    def test_timeout_stops_commands_and_skips_save(self):
        session = FakeSession({"first": False})
        with self.assertRaises(OperationError):
            execute(
                Request((Command("first"), Command("second")), save=True), session, io.StringIO()
            )
        self.assertEqual(session.calls, ["first"])

    def test_initially_disabled_wlan_stays_disabled(self):
        session = FakeSession(states={"1": False})
        execute(Request((CycleWlan("1"), Command("change")), save=True), session, io.StringIO())
        self.assertEqual(session.calls, ["show wlan 1", "change", "show wlan 1", "save config"])
        self.assertFalse(session.states["1"])

    def test_initially_disabled_wlan_is_restored_even_if_command_enables_it(self):
        session = FakeSession(states={"1": False})
        execute(
            Request((CycleWlan("1"), Command("config wlan enable 1")), save=True),
            session,
            io.StringIO(),
        )
        self.assertEqual(
            session.calls[-3:],
            [
                "config wlan disable 1",
                "show wlan 1",
                "save config",
            ],
        )
        self.assertFalse(session.states["1"])

    def test_unknown_initial_state_never_changes_or_saves(self):
        session = FakeSession({"show wlan 1": OperationError("missing WLAN")})
        with self.assertRaises(OperationError):
            execute(Request((CycleWlan("1"),), save=True), session, io.StringIO())
        self.assertEqual(session.calls, ["show wlan 1"])

    def test_disable_must_be_verified_before_configuration(self):
        session = FakeSession(unchanged={"config wlan disable 1"})
        with self.assertRaisesRegex(OperationError, "still enabled"):
            execute(Request((CycleWlan("1"), Command("change")), save=True), session, io.StringIO())
        self.assertNotIn("change", session.calls)
        self.assertNotIn("save config", session.calls)

    def test_unverified_restoration_stops_next_cycle_and_save(self):
        session = FakeSession(unchanged={"config wlan enable 1"})
        with self.assertRaisesRegex(OperationError, "original state"):
            execute(Request((CycleWlan("1"), CycleWlan("2")), save=True), session, io.StringIO())
        self.assertNotIn("show wlan 2", session.calls)
        self.assertNotIn("save config", session.calls)

    def test_controller_error_restores_but_stops_remaining_commands(self):
        session = FakeSession({"change": OperationError("controller rejected change")})
        with self.assertRaises(OperationError):
            execute(
                Request((CycleWlan("1"), Command("change"), Command("next")), save=True),
                session,
                io.StringIO(),
            )
        self.assertTrue(session.states["1"])
        self.assertNotIn("next", session.calls)
        self.assertNotIn("save config", session.calls)

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
            self.assertIn("lab\t192.0.2.1\toperator\twlc", out.getvalue())
            self.assertNotIn("test-secret", out.getvalue())

    def test_ap_target_refuses_controller_only_options_before_connecting(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "devices.json"
            path.write_text(
                json.dumps(
                    {
                        "ap1": {
                            "kind": "ap",
                            "host": "192.0.2.17",
                            "username": "admin",
                            "password": "test-secret",
                        }
                    }
                ),
                encoding="utf-8",
            )
            for request in (
                Request((CycleWlan("1"), Command("show version")), "ap1", str(path)),
                Request((Command("show version"),), "ap1", str(path), save=True),
            ):
                with self.subTest(request=request), self.assertRaisesRegex(UsageError, "AP"):
                    run(
                        request,
                        env={},
                        open_session=lambda *args: self.fail("unexpected connection"),
                    )
            # Plain commands reach the AP session with the kind resolved.
            session = FakeSession()
            targets = []

            def connect(target, out, err):
                targets.append(target)
                return session

            out = io.StringIO()
            run(
                Request((Command("show version"),), "ap1", str(path)),
                env={},
                out=out,
                open_session=connect,
            )
            self.assertTrue(targets[0].is_ap)
            self.assertEqual(targets[0].enable_password, "test-secret")
            self.assertEqual(session.calls, ["show version", "close"])
            run(
                Request(inventory=str(path), list_devices=True),
                env={},
                out=out,
                open_session=lambda *args: self.fail("unexpected connection"),
            )
            self.assertIn("ap1\t192.0.2.17\tadmin\tap", out.getvalue())

    def test_list_does_not_show_a_bad_kind_value(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "devices.json"
            path.write_text(
                json.dumps(
                    {"odd": {"host": "192.0.2.1", "user": "operator", "kind": "typo-value"}}
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
            self.assertIn("odd\t192.0.2.1\toperator\t(unknown kind)", out.getvalue())
            self.assertNotIn("typo-value", out.getvalue())

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
