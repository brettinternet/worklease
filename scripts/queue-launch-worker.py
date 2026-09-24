#!/usr/bin/env python3
"""Reference queue handoff: verify the worker authority, then acquire exact keys.

This example only acquires a claim. A real worker must maintain its own heartbeat,
checkpoint provider progress, and release its own claim; the queue does none of that.
"""

import json
import os
import subprocess
import sys
import uuid


def main():
    expected = os.environ.get("WORKLEASE_QUEUE_AUTHORITY_ID", "")
    resources = json.loads(os.environ.get("WORKLEASE_QUEUE_RESOURCES", "null"))
    if not expected or not isinstance(resources, list) or not 1 <= len(resources) <= 32 or any(
        not isinstance(key, str) or not key or "\n" in key for key in resources
    ):
        raise ValueError("invalid queue authority or resources")
    observed = subprocess.run(
        ["worklease", "queue", "authority-id", "--json"], check=True, capture_output=True, text=True
    )
    authority = json.loads(observed.stdout)["authorityId"]
    if authority != expected:
        raise ValueError("authority-mismatch: worker selected a different authority")
    session = str(uuid.uuid4())
    command = ["worklease", "acquire", "--json", "--session", session]
    for key in resources:
        command.extend(["--resource", key])
    acquired = subprocess.run(command, check=True, capture_output=True, text=True)
    claim = json.loads(acquired.stdout)
    if claim["authorityId"] != expected or len(claim["resources"]) != len(resources):
        raise ValueError("acquired claim does not match handoff; inspect private handle before recovery")
    # Public output may redact portable generic keys. Verify each exact key via
    # the private contextual handle rather than comparing redacted JSON.
    for key in resources:
        subprocess.run(
            ["worklease", "verify", "--session", session, "--resource", key],
            check=True, capture_output=True, text=True,
        )
    print(json.dumps({"authorityId": authority, "resources": resources, "sessionId": session}))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"queue launcher refused: {error}", file=sys.stderr)
        sys.exit(1)
