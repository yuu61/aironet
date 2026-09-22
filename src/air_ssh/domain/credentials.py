"""Device credentials take precedence over the legacy global WLC_PASS."""

from collections.abc import Mapping

from .errors import UsageError
from .inventory import entry_text


def resolve_password(entry: Mapping, env: Mapping[str, str]) -> str:
    password = entry_text(entry, "password")
    if password:
        return password
    variable = entry_text(entry, "password_env")
    if variable and env.get(variable):
        return env[variable]
    if env.get("WLC_PASS"):
        return env["WLC_PASS"]
    raise UsageError('no password available; add "password" or "password_env" to devices.json')


def resolve_enable_password(entry: Mapping, env: Mapping[str, str], password: str) -> str:
    """The AP's privileged EXEC secret; the login password when none is configured."""
    secret = entry_text(entry, "enable_password", "enable_secret")
    if secret:
        return secret
    variable = entry_text(entry, "enable_password_env")
    if variable and env.get(variable):
        return env[variable]
    return password
