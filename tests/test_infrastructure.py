import io
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from air_ssh.domain import OperationError, Target, UsageError
from air_ssh.infrastructure.inventory import inventory_path, read_inventory
from air_ssh.infrastructure.session import NetmikoSession, open_session


class InventoryFileTests(unittest.TestCase):
    def test_path_precedence_and_default(self):
        self.assertEqual(
            inventory_path("flag.json", {"AIRONET_INVENTORY": "env.json"}), Path("flag.json")
        )
        self.assertEqual(inventory_path(None, {"AIRONET_INVENTORY": "env.json"}), Path("env.json"))
        self.assertEqual(inventory_path(None, {}), Path.home() / ".aironet" / "devices.json")

    def test_missing_default_is_empty_but_explicit_missing_is_error(self):
        with (
            tempfile.TemporaryDirectory() as directory,
            patch("pathlib.Path.home", return_value=Path(directory)),
        ):
            self.assertEqual(read_inventory(env={})[0], {})
            for override, env in (
                (str(Path(directory) / "missing"), {}),
                (None, {"AIRONET_INVENTORY": str(Path(directory) / "missing")}),
            ):
                with self.assertRaises(UsageError):
                    read_inventory(override, env)

    def test_bom_supported_and_bad_input_redacted(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "inventory.json"
            path.write_text('{"lab": {"host": "192.0.2.1"}}', encoding="utf-8-sig")
            self.assertIn("lab", read_inventory(str(path), {})[0])
            for data in (b'{"password": "test-secret" BAD}', b"test-secret\xff"):
                path.write_bytes(data)
                with self.assertRaises(UsageError) as raised:
                    read_inventory(str(path), {})
                self.assertNotIn("test-secret", str(raised.exception))


class Channel:
    def __init__(self, chunks):
        self.chunks = iter(chunks)
        self.writes = []

    def write_channel(self, text):
        self.writes.append(text)

    def read_channel(self):
        return next(self.chunks, "")


class SessionTests(unittest.TestCase):
    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_split_confirmation_and_pagination(self, sleep):
        channel = Channel(
            [
                "Proceed (y/",
                "n)",
                "",
                "Press Ent",
                "er to continue",
                "",
                "\n(Cisco Controller) >",
                "",
                "",
            ]
        )
        out = io.StringIO()
        self.assertTrue(NetmikoSession(channel, out, io.StringIO()).run("show run-config"))
        self.assertEqual(channel.writes, ["show run-config\n", "y\n", "\n"])
        self.assertIn("Proceed (y/n)", out.getvalue())

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_prompt_before_echo_does_not_end_output(self, sleep):
        channel = Channel(
            ["(Cisco Controller) >", "", "show sysinfo\nresult\n", "(Cisco Controller) >", "", ""]
        )
        out = io.StringIO()
        self.assertTrue(NetmikoSession(channel, out, io.StringIO()).run("show sysinfo"))
        self.assertIn("result", out.getvalue())

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_inactivity_resets_when_data_arrives(self, sleep):
        channel = Channel(["data\n", "", "more\n", "", "(Cisco Controller) >", "", ""])
        with patch(
            "air_ssh.infrastructure.session.time.monotonic",
            side_effect=[0, 100, 150, 200, 250, 300],
        ):
            self.assertTrue(
                NetmikoSession(channel, io.StringIO(), io.StringIO()).run("show run-config")
            )

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_silent_channel_times_out(self, sleep):
        err = io.StringIO()
        with patch("air_ssh.infrastructure.session.time.monotonic", side_effect=[0, 121]):
            self.assertFalse(NetmikoSession(Channel([]), io.StringIO(), err).run("show sysinfo"))
        self.assertIn("no output for 120s", err.getvalue())

    @patch("netmiko.ConnectHandler")
    def test_connection_uses_inventory_values(self, connect):
        connect.return_value.read_channel.side_effect = ["(Cisco Controller) >", "", ""]
        session = open_session(
            Target("lab", "192.0.2.1", "operator", "test-secret", 2222),
            io.StringIO(),
            io.StringIO(),
        )
        connect.assert_called_once_with(
            device_type="cisco_wlc_ssh",
            host="192.0.2.1",
            port=2222,
            username="operator",
            password="test-secret",
            fast_cli=False,
        )
        session.close()
        connect.return_value.disconnect.assert_called_once()
        connect.return_value.write_channel.assert_called_once_with("config paging enable\n")

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_controller_errors_are_failures_even_after_long_output(self, sleep):
        for error in (
            "Request failed for wlan 10 - Static WEP key size does not match 802.1X WEP key size",
            "Incorrect usage. Use the '?' or <TAB> key to list commands.",
            "Error: WLAN does not exist",
            "% Invalid input detected",
        ):
            with self.subTest(error=error):
                channel = Channel(
                    [error[:8], error[8:] + "\n", "detail\n" * 100, "(Cisco Controller) >", "", ""]
                )
                with self.assertRaises(OperationError):
                    NetmikoSession(channel, io.StringIO(), io.StringIO()).run(
                        "config wlan enable 10"
                    )

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_normal_output_and_command_echo_do_not_trigger_confirmation(self, sleep):
        channel = Channel(
            [
                "(Cisco Controller) >config wlan create 1 confirm\n",
                "Profile Name..................................... confirm\n",
                "Description.......are you sure (y/n)\n",
                "Error Count...................................... 0\n",
                "(Cisco Controller) >",
                "",
                "",
            ]
        )
        self.assertTrue(
            NetmikoSession(channel, io.StringIO(), io.StringIO()).run(
                "config wlan create 1 confirm"
            )
        )
        self.assertEqual(channel.writes, ["config wlan create 1 confirm\n"])

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_confirmation_like_partial_line_is_not_answered(self, sleep):
        channel = Channel(
            [
                "Would you like to continue (y/n)",
                " is a sample question\n",
                "(Cisco Controller) >",
                "",
                "",
            ]
        )
        self.assertTrue(NetmikoSession(channel, io.StringIO(), io.StringIO()).run("show help"))
        self.assertEqual(channel.writes, ["show help\n"])

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_more_pages_use_space_once_per_prompt(self, sleep):
        channel = Channel(
            [
                "page 1\n--Mo",
                "re-- or (q)uit",
                "",
                "",
                "\rpage 2\n--More-- or (q)uit",
                "",
                "",
                "\rpage 3\n(Cisco Controller) >",
                "",
                "",
            ]
        )
        out = io.StringIO()
        self.assertTrue(NetmikoSession(channel, out, io.StringIO()).run("show sysinfo"))
        self.assertEqual(channel.writes, ["show sysinfo\n", " ", " "])
        self.assertIn("page 3", out.getvalue())

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_save_waits_for_confirmation_success_and_prompt(self, sleep):
        channel = Channel(
            [
                "save config\nAre you sure you want to sa",
                "ve? (y/n)",
                "",
                "y\nConfiguration Sa",
                "ved!\n",
                "",
                "(Cisco Controller) >",
                "",
                "",
            ]
        )
        NetmikoSession(channel, io.StringIO(), io.StringIO()).save()
        self.assertEqual(channel.writes, ["save config\n", "y\n"])

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_save_without_success_marker_fails(self, sleep):
        for response in ("", "Error: saving failed\n"):
            with self.subTest(response=response), self.assertRaises(OperationError):
                NetmikoSession(
                    Channel([response + "(Cisco Controller) >", "", ""]),
                    io.StringIO(),
                    io.StringIO(),
                ).save()

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_save_without_response_does_not_send_yes(self, sleep):
        channel = Channel([])
        with (
            patch("air_ssh.infrastructure.session.time.monotonic", side_effect=[0, 121]),
            self.assertRaises(OperationError),
        ):
            NetmikoSession(channel, io.StringIO(), io.StringIO()).save()
        self.assertEqual(channel.writes, ["save config\n"])

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_save_success_without_final_prompt_is_not_complete(self, sleep):
        channel = Channel(["Configuration Saved!\n", ""])
        with (
            patch("air_ssh.infrastructure.session.time.monotonic", side_effect=[0, 1, 122]),
            self.assertRaises(OperationError),
        ):
            NetmikoSession(channel, io.StringIO(), io.StringIO()).save()

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_save_does_not_confirm_an_unrelated_question(self, sleep):
        channel = Channel(["Proceed with reset? (y/n)", ""])
        with (
            patch("air_ssh.infrastructure.session.time.monotonic", side_effect=[0, 1, 122]),
            self.assertRaises(OperationError),
        ):
            NetmikoSession(channel, io.StringIO(), io.StringIO()).save()
        self.assertEqual(channel.writes, ["save config\n"])

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_bare_echo_of_question_does_not_trigger_reply(self, sleep):
        channel = Channel(["Proceed (y/n)\n", "", "(Cisco Controller) >", "", ""])
        self.assertTrue(NetmikoSession(channel, io.StringIO(), io.StringIO()).run("Proceed (y/n)"))
        self.assertEqual(channel.writes, ["Proceed (y/n)\n"])

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_wlan_status_is_specific_to_requested_id_and_top_level_status(self, sleep):
        for state in ("Enabled", "Disabled"):
            with self.subTest(state=state):
                channel = Channel(
                    [
                        f"WLAN Identifier........ 1\nStatus........ {state}\n",
                        "MAC Filtering......... Disabled\nRadius-NAC State......... Enabled\n",
                        "(Cisco Controller) >",
                        "",
                        "",
                    ]
                )
                self.assertEqual(
                    NetmikoSession(channel, io.StringIO(), io.StringIO()).wlan_enabled("1"),
                    state == "Enabled",
                )

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_unknown_or_ambiguous_wlan_state_is_rejected(self, sleep):
        for output in (
            "WLAN Identifier........ 2\nStatus........ Enabled\n",
            "WLAN Identifier........ 1\nMAC Filtering........ Enabled\n",
            "WLAN Identifier........ 1\nStatus........ Unknown\n",
            "WLAN Identifier........ 1\nStatus........ Enabled\nStatus........ Disabled\n",
            "WLAN not found\n",
        ):
            with self.subTest(output=output), self.assertRaises(OperationError):
                NetmikoSession(
                    Channel([output + "(Cisco Controller) >", "", ""]), io.StringIO(), io.StringIO()
                ).wlan_enabled("1")

    @patch("air_ssh.infrastructure.session.time.sleep")
    def test_timeout_blocks_further_commands_and_closes_transport_before_logout(self, sleep):
        conn = Mock()
        conn.read_channel.return_value = ""
        session = NetmikoSession(conn, io.StringIO(), io.StringIO())
        with patch("air_ssh.infrastructure.session.time.monotonic", side_effect=[0, 121]):
            self.assertFalse(session.run("show sysinfo"))
        with self.assertRaisesRegex(OperationError, "reconnect"):
            session.run("config wlan enable 1")
        conn.write_channel.assert_called_once_with("show sysinfo\n")
        session.close()
        self.assertEqual(
            [call[0] for call in conn.mock_calls[-2:]], ["paramiko_cleanup", "disconnect"]
        )

    @patch("air_ssh.infrastructure.session.time.sleep")
    @patch("netmiko.ConnectHandler")
    def test_paging_setup_failure_closes_connection(self, connect, sleep):
        connect.return_value.read_channel.side_effect = [
            "Error: Permission denied\n(Cisco Controller) >",
            "",
            "",
        ]
        with self.assertRaises(OperationError):
            open_session(
                Target("lab", "192.0.2.1", "operator", "test-secret"), io.StringIO(), io.StringIO()
            )
        connect.return_value.disconnect.assert_called_once()
