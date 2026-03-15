#!/usr/bin/env python3
"""Validate example JSON files against their JSON schemas.

Usage:
    python3 scripts/validate_schemas.py

Exit code 0 = all examples valid. Non-zero = validation failures.
"""

import json
import sys
from pathlib import Path

try:
    import jsonschema
except ImportError:
    print("ERROR: jsonschema not installed. Run: pip install jsonschema")
    sys.exit(1)

ROOT = Path(__file__).parent.parent
SCHEMAS_DIR = ROOT / "schemas"
EXAMPLES_DIR = ROOT / "examples"

VALIDATIONS = [
    ("canonical_object.schema.json", "canonical_object_example.json"),
    ("agent_reference.schema.json", "agent_reference_example.json"),
]

errors = 0
for schema_file, example_file in VALIDATIONS:
    schema_path = SCHEMAS_DIR / schema_file
    example_path = EXAMPLES_DIR / example_file

    with open(schema_path) as f:
        schema = json.load(f)
    with open(example_path) as f:
        instance = json.load(f)

    try:
        jsonschema.validate(instance, schema, format_checker=jsonschema.FormatChecker())
        print(f"OK   {example_file}")
    except jsonschema.ValidationError as e:
        print(f"FAIL {example_file}: {e.message}")
        errors += 1

if errors == 0:
    print(f"\nAll {len(VALIDATIONS)} examples valid.")
else:
    print(f"\n{errors}/{len(VALIDATIONS)} examples failed validation.")

sys.exit(errors)
