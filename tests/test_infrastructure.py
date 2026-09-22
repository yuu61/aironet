import io
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from air_ssh.domain import Target, UsageError
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
            ["Proceed (y/", "n)", "Press Ent", "er to continue", "\n(Cisco Controller) >", "", ""]
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
        channel = Channel(["data", "", "more", "", "(Cisco Controller) >", "", ""])
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
