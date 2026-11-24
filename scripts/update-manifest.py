"""Create the branch updater manifest from immutable per-commit assets."""
import hashlib,json,os,pathlib,datetime
root=pathlib.Path('dist')
assets={}
for p in root.glob('*'):
    name=p.name
    key=None
    if name.endswith('-macos-arm64.app.zip'): key='darwin-arm64'
    elif name.endswith('-windows-amd64.zip'): key='windows-amd64'
    elif name.endswith('-linux-amd64.tar.gz'): key='linux-amd64'
    if name.endswith('-linux-amd64.AppImage'): key='linux-amd64-appimage'
    if key: assets[key]={'name':name,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
if len(assets)!=4: raise SystemExit(f'Missing update assets: {assets.keys()}')
(root/'update.json').write_text(json.dumps({'commit':os.environ['GITHUB_SHA'],'channel':os.environ['UPDATE_CHANNEL'],'builtAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'assets':assets},indent=2))
