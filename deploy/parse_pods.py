import json, sys
data = json.load(open(sys.argv[1], encoding='utf-16'))
for p in data['items']:
    name = p['metadata']['name']
    podIP = p.get('status', {}).get('podIP', '')
    anns = p['metadata'].get('annotations', {})
    replica = anns.get('app.vortexops.io/replica-index', '')
    stable_ip = anns.get('app.vortexops.io/stable-ip-0', '')
    assigned_by = anns.get('app.vortexops.io/ip-assigned-by', '')
    calico = anns.get('cni.projectcalico.org/ipAddrs', '')
    print(f"{name}  podIP={podIP}  replica={replica}  stable-ip-0={stable_ip}  assigned-by={assigned_by}  calico={calico}")
