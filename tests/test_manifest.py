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
        cls.harness = cls.manifest["harness"]

    def test_extension(self):
        ext = self.manifest["extension"]
        self.assertEqual((ext["id"], ext["name"], ext["publisher"]), ("grokbot", "Grok Bot", "aurelio"))
        self.assertRegex(ext["version"], r"^\d+\.\d+\.\d+$")
        # 0.4.19: an agent with its own grokbot.key.<handle> runs on it (0.4.18: a key form's fields by name)
        self.assertEqual(ext["node"], ">=0.6,<1")

    def test_one_text_run_per_wake_with_the_prompt_on_stdin(self):
        self.assertEqual(self.harness["protocol"], "text")
        self.assertEqual(self.harness["binaries"], ["grokbot-wake"])
        self.assertEqual(self.harness["run"], {"prompt": "stdin", "args": []})
        self.assertNotIn("home_env", self.harness)
        self.assertNotIn("mcp", self.harness)  # the Bot reaches the board through its own connector

    def test_installs_this_repos_release_build(self):
        self.assertEqual(self.harness["install"], {"from": "release", "repo": "pointman-ai/pointman-grokbot",
                                                  "asset": "grokbot-wake-{platform}.tar.gz"})
        build = (ROOT / "scripts/build.sh").read_text()
        for platform in ("darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64"):
            self.assertIn(platform, build)

    def test_one_vault_form_supplies_every_agents_run(self):
        self.assertEqual(self.harness["api_keys"], ["GROKBOT_URL", "GROKBOT_KEY"])
        self.assertEqual(self.harness["key"], {"secret": "grokbot.key", "env": "GROKBOT_KEY"})
        self.assertEqual(self.harness["sign_in"], {"how": "/grokbot add"})
        self.assertEqual(self.manifest["permissions"]["secrets"], ["grokbot.key"])
        (cred,) = self.manifest["credentials"]
        self.assertEqual((cred["id"], cred["item"]), ("webhook", "grokbot.key"))
        (cmd,) = self.manifest["commands"]
        self.assertEqual((cmd["kind"], cmd["slash"], cmd["form"]), ("grokbot.add", "add", cred["id"]))
        self.assertEqual(cmd["params"], {"type": "object", "properties": {}, "additionalProperties": False})

    def test_the_form_takes_a_shown_https_address_and_a_hidden_key(self):
        cred = self.manifest["credentials"][0]
        self.assertNotIn("value", cred)
        url, key = cred["fields"]
        # every field is one the node hands the run: the harness's api_keys name them all
        self.assertEqual([url["name"], key["name"]], self.harness["api_keys"])
        self.assertEqual((url.get("plain"), key.get("plain", False)), (True, False))
        address = re.compile(url["value"])
        for good in ("https://api.x.ai/v1/routines/r_1/webhook", "https://api.x.ai"):
            self.assertTrue(address.fullmatch(good), good)
        # what grokbot-wake's parseHook refuses, the form refuses too
        for bad in ("", "api.x.ai/hook", "http://api.x.ai/hook", "https://user:pw@a.example/hook",
                    "https://a.example/hook gbk_0123456789"):
            self.assertIsNone(address.fullmatch(bad), bad)
        secret = re.compile(key["value"])
        self.assertTrue(secret.fullmatch("gbk_0123456789"))
        for bad in ("", "short", "gbk_0123 456789"):
            self.assertIsNone(secret.fullmatch(bad), bad)

    def test_rate_limits_count_as_the_accounts(self):
        self.assertIn("rate limit", self.harness["limits"]["words"])
        self.assertIn("rate limit", (ROOT / "main.go").read_text())


if __name__ == "__main__":
    unittest.main()
