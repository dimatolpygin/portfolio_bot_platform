#!/usr/bin/env python3
import json
import pathlib
import sys

import yaml
from jsonschema import Draft202012Validator


def main() -> int:
    manifest_path = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "bot.yaml")
    schema_path = pathlib.Path(sys.argv[2] if len(sys.argv) > 2 else "schemas/bot.schema.json")

    manifest = yaml.safe_load(manifest_path.read_text(encoding="utf-8"))
    schema = json.loads(schema_path.read_text(encoding="utf-8"))

    validator = Draft202012Validator(schema)
    errors = sorted(validator.iter_errors(manifest), key=lambda error: list(error.path))
    if not errors:
        print(f"{manifest_path} is valid")
        return 0

    for error in errors:
        location = ".".join(str(item) for item in error.path) or "<root>"
        print(f"{location}: {error.message}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())

