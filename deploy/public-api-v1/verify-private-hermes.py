#!/usr/bin/env python3
"""Bounded read-only probe inside the existing old Hermes container.

Outputs metadata/booleans only; no key, prompt, response, config or user data.
"""
import json
import os
from pathlib import Path
import subprocess
import sys

PROBE = r'''
import hashlib,json,os,pathlib,urllib.request,urllib.error
base=os.environ["OPENAI_BASE_URL"].rstrip("/")
key=os.environ["OPENAI_API_KEY"]
assert base=="http://cli-proxy-api:8317/v1"
def call(path,payload=None):
 req=urllib.request.Request(base+path,data=None if payload is None else json.dumps(payload).encode(),headers={"Authorization":"Bearer "+key,"Content-Type":"application/json"})
 try:
  with urllib.request.urlopen(req,timeout=120) as response:
   return response.status,dict(response.headers),json.load(response)
 except urllib.error.HTTPError as error:
  return error.code,{},{}
status,headers,catalog=call("/models")
print(json.dumps({"catalog_status":status,"model_present":any(x.get("id")=="gemini-3.8-flash-tiered" for x in catalog.get("data",[])),"capabilities":{k:v for k,v in headers.items() if k.lower().startswith("x-clirelay-")},"config_sha256":hashlib.sha256(pathlib.Path("/opt/data/config.yaml").read_bytes()).hexdigest(),"key_sha256":hashlib.sha256(key.encode()).hexdigest()}))
status,_,answer=call("/chat/completions",{"model":"gemini-3.8-flash-tiered","messages":[{"role":"user","content":"Reply exactly: PRIVATE_OK"}],"reasoning_effort":"medium","stream":False,"max_tokens":64})
text=answer.get("choices",[{}])[0].get("message",{}).get("content","")
print(json.dumps({"inference_status":status,"synthetic_marker": "PRIVATE_OK" in str(text)}))
'''

if __name__ == "__main__":
    if sys.argv[1:] not in (["--baseline"], ["--compare"]):
        raise SystemExit("use --baseline or --compare on the VPS")
    result = subprocess.run(["docker", "exec", "-i", "hermes", "python", "-c", PROBE], capture_output=True, text=True)
    if result.returncode:
        raise SystemExit("private Hermes probe failed; raw output suppressed")
    records = [json.loads(line) for line in result.stdout.splitlines()]
    # Fingerprints are kept server-side for comparison, not displayed.
    current = os.environ.get("CLIRELAY_VERIFY_BACKUP_CURRENT_FILE", "/root/clirelay-public-v1-backups/current")
    directory = Path(Path(current).read_text())
    baseline = directory / "private-hermes-baseline.json"
    result = {k: v for record in records for k, v in record.items()}
    if sys.argv[1] == "--baseline":
        baseline.write_text(json.dumps(result))
        baseline.chmod(0o600)
    else:
        old = json.loads(baseline.read_text())
        result["private_contract_unchanged"] = all(result.get(k) == old.get(k) for k in ["config_sha256", "key_sha256", "capabilities", "model_present"])
        if not result["private_contract_unchanged"]:
            raise SystemExit("private Hermes contract changed")
    print(json.dumps({k: v for k, v in result.items() if not k.endswith("sha256")}))
    if result.get("catalog_status") != 200 or result.get("inference_status") != 200 or not result.get("synthetic_marker"):
        raise SystemExit("private Hermes inference did not pass")
