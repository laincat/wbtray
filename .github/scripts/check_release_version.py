"""Reject release tags that do not identify the compiled Cargo version."""
import pathlib
import sys
import tomllib


def validate(tag, version):
    if tag != f"v{version}":
        raise ValueError(f"tag {tag!r} does not match Cargo version v{version}")


if __name__ == "__main__":
    manifest = tomllib.loads(pathlib.Path("Cargo.toml").read_text(encoding="utf-8"))
    validate(sys.argv[1], manifest["package"]["version"])
