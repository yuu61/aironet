import unittest

from air_ssh.domain import CycleWlan, UsageError, parse_inventory, resolve_target, select_entry
from air_ssh.domain.credentials import resolve_password


class InventoryTests(unittest.TestCase):
    def test_wrapped_and_bare_inventory_ignore_comments(self):
        devices = {"_comment": "local only", "lab": {"host": "192.0.2.1"}}
        expected = {"lab": {"host": "192.0.2.1"}}
        self.assertEqual(parse_inventory(devices, "test"), expected)
        self.assertEqual(parse_inventory({"devices": devices}, "test"), expected)

    def test_invalid_inventory_does_not_leak_values(self):
        for value in ([], {"devices": []}, {"lab": "sensitive-value"}):
            with self.subTest(value=value), self.assertRaises(UsageError) as raised:
                parse_inventory(value, "test")
            self.assertNotIn("sensitive-value", str(raised.exception))

    def test_device_is_explicit_and_cli_beats_environment(self):
        devices = {"a": {}, "b": {}}
        self.assertEqual(select_entry("a", devices, {"AIRONET_DEVICE": "b"})[0], "a")
        self.assertEqual(select_entry(None, devices, {"AIRONET_DEVICE": "b"})[0], "b")
        for name in (None, "unknown"):
            with self.assertRaises(UsageError):
                select_entry(name, devices, {})

    def test_password_precedence(self):
        env = {"LAB_PASS": "per-device", "WLC_PASS": "global"}
        entry = {"password": "inventory-value", "password_env": "LAB_PASS"}
        self.assertEqual(resolve_password(entry, env), "inventory-value")
        self.assertEqual(resolve_password({"password_env": "LAB_PASS"}, env), "per-device")
        self.assertEqual(resolve_password({}, env), "global")
        with self.assertRaises(UsageError):
            resolve_password({"password_env": "MISSING"}, {})

    def test_target_aliases_port_and_hidden_password(self):
        target = resolve_target(
            "lab",
            {"ip": "192.0.2.1", "user": "operator", "port": "2222", "password": "test-secret"},
            {},
        )
        self.assertEqual(
            (target.host, target.username, target.port), ("192.0.2.1", "operator", 2222)
        )
        self.assertNotIn("test-secret", repr(target))

    def test_invalid_fields_fail_before_connecting(self):
        valid = {"host": "192.0.2.1", "username": "operator", "password": "test-secret"}
        for field, value in (
            ("host", ""),
            ("username", []),
            ("password", 42),
            ("password", None),
            ("port", 0),
            ("port", 65536),
            ("port", True),
            ("port", 22.5),
        ):
            with self.subTest(field=field, value=value), self.assertRaises(UsageError):
                resolve_target("lab", {**valid, field: value}, {})

    def test_wlan_id_cannot_inject_commands(self):
        for value in ("", "0", "-1", "1\nsave config", "1 extra", "１"):
            with self.subTest(value=value), self.assertRaises(UsageError):
                CycleWlan(value)
