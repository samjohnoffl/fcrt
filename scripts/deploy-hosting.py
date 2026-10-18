#!/usr/bin/env python3
"""Deploy frontend/dist to Firebase Hosting through the Hosting REST API.

`firebase-tools` is not always installable (the CLI download host may be blocked,
and it pulls a large dependency tree), but the Hosting API is a plain REST
service, so this script deploys a release directly. It is also useful in CI
where only a `gcloud` service account is available.

Prerequisites, once per GCP project:

    gcloud services enable firebasehosting.googleapis.com
    curl -X POST -H "Authorization: Bearer $(gcloud auth print-access-token)" \
         -H "x-goog-user-project: $PROJECT" \\
         "https://firebase.googleapis.com/v1beta1/projects/$PROJECT:addFirebase" -d '{}'

Usage:

    python3 scripts/deploy-hosting.py [--project ID] [--site ID] [--dist DIR]

Defaults to the project in `.firebaserc`, a site named after the project, and
`frontend/dist`.

Two details of the API are easy to get wrong and are handled here:

1. The version `config` uses a *different* dialect from `firebase.json`. The file
   uses `source`/`destination` and a list of `{key, value}` headers, while the
   API expects `glob`/`path` and a header *map*. This script builds the API
   dialect.
2. Uploaded file bodies must be gzipped, and the SHA-256 declared in
   `populateFiles` must be the hash of the *gzipped* bytes — not of the file on
   disk. Hosting verifies the hash of exactly what was uploaded.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

API = "https://firebasehosting.googleapis.com/v1beta1"
UPLOAD_API = "https://upload-firebasehosting.googleapis.com/upload"
REPO_ROOT = Path(__file__).resolve().parent.parent

# Cache-Control rules applied to every release. Order matters: later matches win,
# so the default is no-store and the hashed bundle is immutable.
HEADER_RULES = [
    ("**", "no-cache, no-store, must-revalidate"),
    ("**/*.@(js|css|woff2|woff|png|jpg|svg|ico)", "public, max-age=31536000, immutable"),
]


def access_token() -> str:
    try:
        out = subprocess.run(
            ["gcloud", "auth", "print-access-token"],
            capture_output=True, text=True, check=True,
        )
    except (FileNotFoundError, subprocess.CalledProcessError) as exc:
        sys.exit(f"could not get an access token from gcloud: {exc}")
    return out.stdout.strip()


def call(method: str, url: str, token: str, project: str, payload=None, raw=None):
    data = raw if raw is not None else (
        json.dumps(payload).encode() if payload is not None else None
    )
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("x-goog-user-project", project)
    if data is not None:
        req.add_header(
            "Content-Type",
            "application/octet-stream" if raw is not None else "application/json",
        )
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            body = resp.read()
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode(errors="replace")[:800]
        sys.exit(f"{method} {url} -> HTTP {exc.code}\n{detail}")


def collect(dist: Path) -> tuple[dict[str, str], dict[str, bytes]]:
    """Map each deployed path to the hash and bytes of its gzipped form."""
    if not dist.is_dir():
        sys.exit(f"build output not found: {dist} — run `npm run build` first")

    hashes: dict[str, str] = {}
    bodies: dict[str, bytes] = {}
    for path in sorted(dist.rglob("*")):
        if not path.is_file():
            continue
        rel = "/" + path.relative_to(dist).as_posix()
        gzipped = gzip.compress(path.read_bytes(), mtime=0)
        hashes[rel] = hashlib.sha256(gzipped).hexdigest()
        bodies[rel] = gzipped
    return hashes, bodies


def default_project() -> str:
    rc = REPO_ROOT / ".firebaserc"
    if rc.is_file():
        try:
            return json.loads(rc.read_text())["projects"]["default"]
        except (KeyError, json.JSONDecodeError):
            pass
    sys.exit("no --project given and .firebaserc has no projects.default")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--project", default=os.environ.get("FIREBASE_PROJECT"))
    parser.add_argument("--site", default=os.environ.get("FIREBASE_SITE"))
    parser.add_argument("--dist", default=os.environ.get("FIREBASE_DIST"))
    args = parser.parse_args()

    project = args.project or default_project()
    site = args.site or project
    dist = Path(args.dist) if args.dist else REPO_ROOT / "frontend" / "dist"

    token = access_token()
    hashes, bodies = collect(dist)
    print(f"deploying {len(hashes)} files from {dist} to {site}")

    config = {
        # No /api/** rewrite: the app targets its API by absolute origin.
        "rewrites": [{"glob": "**", "path": "/index.html"}],
        "headers": [{"glob": g, "headers": {"Cache-Control": v}} for g, v in HEADER_RULES],
    }

    version = call("POST", f"{API}/sites/{site}/versions", token, project, {"config": config})
    version_name = version["name"]
    version_id = version_name.rsplit("/", 1)[-1]
    print("created version:", version_id)

    populated = call(
        "POST",
        f"{API}/sites/{site}/versions/{version_id}:populateFiles",
        token, project, {"files": hashes},
    )
    upload_url = populated.get(
        "uploadUrl", f"{UPLOAD_API}/sites/{site}/versions/{version_id}/files"
    )
    required = set(populated.get("uploadRequiredHashes", []))
    print(f"uploading {len(required)} of {len(hashes)} files (the rest are unchanged)")

    by_hash = {hashes[path]: body for path, body in bodies.items()}
    for digest in sorted(required):
        call("POST", f"{upload_url}/{digest}", token, project, raw=by_hash[digest])

    call(
        "PATCH",
        f"{API}/sites/{site}/versions/{version_id}?updateMask=status",
        token, project, {"status": "FINALIZED"},
    )
    release = call(
        "POST",
        f"{API}/sites/{site}/releases?versionName={version_name}",
        token, project,
    )
    print("released:", release.get("name"))
    print(f"live at https://{site}.web.app")


if __name__ == "__main__":
    main()
