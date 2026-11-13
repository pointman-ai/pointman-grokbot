"""Offline contract checks. These never call Grok Bot."""
from pathlib import Path
import re
import tomllib
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ManifestContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.manifest = tomllib.loads((ROOT / "extension.toml").read_text())
        cls.client = cls.manifest["client"]

    def test_extension(self):
        ext = self.manifest["extension"]
        self.assertEqual((ext["id"], ext["name"], ext["publisher"]), ("grokbot", "Grok Bot", "aurelio"))
        self.assertRegex(ext["version"], r"^\d+\.\d+\.\d+$")
        # 0.4.17: a client's vault key reaches its run as the key, not the vault's whole record
        self.assertEqual(ext["node"], ">=0.4.17")

    def test_one_text_run_per_wake_with_the_prompt_on_stdin(self):
        self.assertEqual(self.client["protocol"], "text")
        self.assertEqual(self.client["binaries"], ["grokbot-wake"])
        self.assertEqual(self.client["run"], {"prompt": "stdin", "args": []})
        self.assertNotIn("home_env", self.client)
        self.assertNotIn("mcp", self.client)  # the Bot reaches the board through its own connector

    def test_installs_this_repos_release_build(self):
        self.assertEqual(self.client["install"], {"from": "release", "repo": "pointman-ai/pointman-grokbot",
                                                  "asset": "grokbot-wake-{platform}.tar.gz"})
        build = (ROOT / "scripts/build.sh").read_text()
        for platform in ("darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64"):
            self.assertIn(platform, build)

    def test_one_vault_form_supplies_every_agents_run(self):
        self.assertEqual(self.client["api_keys"], ["GROKBOT_WEBHOOK"])
        self.assertEqual(self.client["key"], {"secret": "grokbot.key", "env": "GROKBOT_WEBHOOK"})
        self.assertEqual(self.client["sign_in"], {"how": "/grokbot add"})
        self.assertEqual(self.manifest["permissions"]["secrets"], ["grokbot.key"])
        (cred,) = self.manifest["credentials"]
        self.assertEqual((cred["id"], cred["item"]), ("webhook", "grokbot.key"))
        (cmd,) = self.manifest["commands"]
        self.assertEqual((cmd["kind"], cmd["slash"], cmd["form"]), ("grokbot.add", "add", cred["id"]))
        self.assertEqual(cmd["params"], {"type": "object", "properties": {}, "additionalProperties": False})

    def test_the_form_takes_an_https_address_then_a_key(self):
        value = re.compile(self.manifest["credentials"][0]["value"])
        for good in ("https://api.x.ai/v1/routines/r_1/webhook gbk_0123456789",
                     "https://api.x.ai/v1/routines/r_1/webhook  gbk_0123456789", "https://api.x.ai gbk_0123456789"):
            self.assertTrue(value.fullmatch(good), good)
        # what grokbot-wake's parseHook refuses, the form refuses too
        for bad in ("https://api.x.ai/hook", "gbk_0123456789", "http://api.x.ai/hook gbk_0123456789",
                    "https://api.x.ai/hook short", "https://a.example/hook key1234 extra",
                    "https://user:pw@a.example/hook key12345678"):
            self.assertIsNone(value.fullmatch(bad), bad)

    def test_rate_limits_count_as_the_accounts(self):
        self.assertIn("rate limit", self.client["limits"]["words"])
        self.assertIn("rate limit", (ROOT / "main.go").read_text())


if __name__ == "__main__":
    unittest.main()
