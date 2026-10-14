#!/usr/bin/env bash
# Opt-in, real public-data integration. No downloads are committed.
set -euo pipefail
cd "$(dirname "$0")/.."
root=$(pwd)
work=$(mktemp -d)
server_pid=''
cleanup() { if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi; if [[ "${WEER_KEEP_SMOKE:-0}" == 1 ]]; then echo "Smoke artifacts: $work"; else rm -rf "$work"; fi; }
trap cleanup EXIT
python_bin=${WEER_PYTHON:-$root/.venv/bin/python}
for entry in \
 'KFTG20240520_000059_V06 a489988121ecab65f4dbe09e13f63cd563967294dac981a9c34a0dfd02584c80' \
 'KFTG20240520_000528_V06 dcc76035ffe66a24642c88800c7275c7a2b84c9dbfd8a44c1c29cf96b1124f71'; do
 read -r name checksum <<< "$entry"
 curl --fail --location --max-time 120 "https://unidata-nexrad-level2.s3.amazonaws.com/2024/05/20/KFTG/$name" -o "$work/$name"
 "$python_bin" -c 'import hashlib,sys; assert hashlib.file_digest(open(sys.argv[1],"rb"),"sha256").hexdigest()==sys.argv[2]' "$work/$name" "$checksum"
done
"$python_bin" - "$work" "$root" "$python_bin" <<'PY'
import sys,yaml
from pathlib import Path
work,root,python=sys.argv[1:]
c=yaml.safe_load(Path('configs/weer.yaml').read_text())
c.update(data_dir=work+'/data',database=work+'/data/weer.db',web_dir=root+'/web/dist',python=python,listen='127.0.0.1:18080')
Path(work+'/config.yaml').write_text(yaml.safe_dump(c))
PY
for volume in "$work"/KFTG*; do ./weer ingest --config "$work/config.yaml" --file "$volume"; done
./weer ingest --config "$work/config.yaml" --file "$work/KFTG20240520_000059_V06"
for round in 1 2; do
 ./weer serve --config "$work/config.yaml" >"$work/server-$round.log" 2>&1 & server_pid=$!
 for attempt in $(seq 1 50); do if curl -fsS http://127.0.0.1:18080/healthz >/dev/null; then break; fi; sleep .2; done
 "$python_bin" - "$work" "$round" <<'PY'
import json,sys,urllib.request
from pathlib import Path
from PIL import Image
from io import BytesIO
base='http://127.0.0.1:18080'
def get(path):
    return urllib.request.urlopen(base+path,timeout=10).read()
scans=json.loads(get('/api/v1/stations/KFTG/scans?from=2024-05-20T00:00:00Z&to=2024-05-21T00:00:00Z'))
assert len(scans)==2, scans
frames=[]
for scan in scans:
    assert scan['state']=='ready',scan
    fid=scan['frame_id'];frames.append(fid)
    m=json.loads(get('/api/v1/frames/'+fid+'/manifest'))
    assert m['station']['id']=='KFTG' and m['projection']=='EPSG:3857'
    assert m['has_valid_measurements'] and m['elevation']<.6
    im=Image.open(BytesIO(get('/api/v1/frames/'+fid+'/image.png')))
    assert im.size==(1024,1024) and im.mode=='RGBA'
    assert im.getchannel('A').getextrema()==(0,255)
    print(scan['id'],m['sweep_time'],m['elevation'])
path=Path(sys.argv[1])/'frame-ids.json'
if sys.argv[2]=='1':path.write_text(json.dumps(frames))
else:assert json.loads(path.read_text())==frames,'Restart changed frame identity'
assert get('/').startswith(b'<!doctype html>')
PY
 kill "$server_pid"; wait "$server_pid"; server_pid=''
done
echo 'PASS: two real decodes, validated PNG/API, duplicate import reuse, and restart persistence.'
