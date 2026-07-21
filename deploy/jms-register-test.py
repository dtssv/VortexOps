import urllib.request, json, sys
core = "http://jumpserver-core:8080"
token = "jumpserver-dev-bootstrap-token"
url = core + "/api/v1/terminal/terminal-registrations/"
body = json.dumps({
    "name": "koko-debug-test",
    "comment": "Koko component debug",
    "type": "koko"
}).encode()
req = urllib.request.Request(url, data=body, headers={
    "Content-Type": "application/json",
    "Authorization": "BootstrapToken " + token,
}, method="POST")
try:
    r = urllib.request.urlopen(req, timeout=10)
    resp = r.read().decode()
    print("REGISTER OK", r.status)
    print(resp[:800])
except urllib.error.HTTPError as e:
    print("REGISTER HTTP", e.code, ":")
    print(e.read().decode()[:800])
except Exception as e:
    print("REGISTER FAIL:", repr(e))
sys.stdout.flush()
