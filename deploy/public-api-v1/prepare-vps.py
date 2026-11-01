#!/usr/bin/env python3
"""Explicitly authorized root-only backup and disabled laptop-key provisioning.

Run on the VPS, never copy the backup off it. No credentials are printed.
The application owns the external-usage table migration at startup.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import sqlite3
import subprocess
import sys
import uuid


def main():
    if os.geteuid() != 0 or sys.argv[1:] != ["--apply"]:
        raise SystemExit("requires root and explicit --apply")
    os.umask(0o077)
    root = Path("/opt/CliRelay")
    db_path = root / "data/usage.db"
    name = "Codex — ноутбук"
    db = sqlite3.connect(db_path, timeout=30)
    assert not db.execute("SELECT 1 FROM api_keys WHERE name=?", (name,)).fetchone(), "laptop key already exists"
    old_auths = json.loads(Path("/root/clirelay-account-backups/20261028T051857Z-before-add-antigravity/baseline.json").read_text())
    candidates = []
    for path in (root / "auths").glob("*.json"):
        auth = json.loads(path.read_text())
        if path.name not in old_auths and auth.get("type") == "antigravity" and not auth.get("disabled"):
            candidates.append(auth)
    assert len(candidates) == 1 and candidates[0].get("email"), "new Antigravity account must be unambiguous"
    channel = candidates[0]["email"]
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup = Path("/root/clirelay-public-v1-backups") / stamp
    backup.mkdir(parents=True, mode=0o700)
    backup_db = sqlite3.connect(backup / "usage.db")
    db.backup(backup_db)
    assert backup_db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
    backup_db.close()
    for source in [root / "config.yaml", root / "docker-compose.yml", Path("/srv/tg-paid-community-demo/Caddyfile"), Path("/srv/tg-paid-community-demo/compose.yaml")]:
        target = backup / ("caddy-compose.yaml" if source.name == "compose.yaml" else source.name)
        shutil.copyfile(source, target)
        target.chmod(0o600)
    baseline = {}
    for service in ["hermes", "cli-proxy-api", "hermes-staging", "xray-reality", "tg-paid-demo-ingress-1"]:
        d = json.loads(subprocess.check_output(["docker", "inspect", service]))[0]
        baseline[service] = {"image": d["Config"]["Image"], "image_id": d["Image"], "started": d["State"]["StartedAt"], "restarts": d["RestartCount"], "ports": d["HostConfig"]["PortBindings"], "networks": list(d["NetworkSettings"]["Networks"])}
    baseline["private_key_rows_sha256"] = hashlib.sha256(repr(db.execute("SELECT * FROM api_keys ORDER BY id").fetchall()).encode()).hexdigest()
    (backup / "baseline.json").write_text(json.dumps(baseline, indent=2))
    (Path("/root/clirelay-public-v1-backups") / "current").write_text(str(backup))
    now = datetime.datetime.now(datetime.timezone.utc).isoformat()
    profile_id, key_id = str(uuid.uuid4()), str(uuid.uuid4())
    with db:
        columns = {r[1] for r in db.execute("PRAGMA table_info(api_keys)")}
        if "external_access" not in columns:
            db.execute("ALTER TABLE api_keys ADD COLUMN external_access INTEGER NOT NULL DEFAULT 0")
        policy = (300, 2, 10, json.dumps(["gemini-3.8-flash-tiered"]), json.dumps([channel]))
        db.execute("INSERT INTO api_key_permission_profiles (id,name,daily_limit,concurrency_limit,rpm_limit,allowed_models,allowed_channels,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)", (profile_id, name, *policy, now, now))
        db.execute("INSERT INTO api_keys (key,id,name,permission_profile_id,daily_limit,concurrency_limit,rpm_limit,allowed_models,allowed_channels,created_at,updated_at,external_access) VALUES (?,?,?,?,?,?,?,?,?,?,?,0)", ("sk-" + secrets.token_urlsafe(36), key_id, name, profile_id, *policy, now, now))
    assert db.execute("SELECT COUNT(*) FROM api_keys WHERE external_access != 0").fetchone()[0] == 0
    db.close()
    for directory in [root / "data", root / "logs", root / "auths"]:
        directory.chmod(0o700)
    for path in list((root / "data").glob("usage.db*")) + list((root / "auths").glob("*.json")) + [root / "config.yaml"]:
        path.chmod(0o600)
    print(json.dumps({"backup_integrity": "ok", "backup_root_only": True, "laptop_key_created": True, "external_access": False, "new_account_selected": True}))


if __name__ == "__main__":
    main()
