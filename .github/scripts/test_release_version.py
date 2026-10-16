import unittest
from check_release_version import validate


class VersionTests(unittest.TestCase):
    def test_matching_tag(self):
        validate("v3.0.0", "3.0.0")

    def test_mismatches(self):
        for tag in ["v2.0.2", "v3.0.0-extra", "3.0.0", "v", "v3.0.1"]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                validate(tag, "3.0.0")
