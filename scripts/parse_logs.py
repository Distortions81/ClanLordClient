import re, base64, json, glob, os

event_re = re.compile(r'^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) (send|recv) (tcp|udp) tag (\d+) len (\d+)')
size_re = re.compile(r'^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} (send|recv) \d+ bytes')
hex_re = re.compile(r'^[0-9a-f]{8}')
byte_re = re.compile(r'([0-9a-f]{2})')

output = []
for path in sorted(glob.glob(os.path.join('go_client','debug-*.log'))):
    with open(path) as f:
        lines = f.readlines()
    i = 0
    while i < len(lines) and len(output) < 3:
        m = event_re.match(lines[i])
        if not m:
            i += 1
            continue
        ts, direction, proto, tag, length = m.groups()
        if not (direction == 'recv' and proto == 'udp' and tag == '2'):
            i += 1
            continue
        i += 1
        if i < len(lines) and size_re.match(lines[i]):
            i += 1
        data = bytearray()
        while i < len(lines) and hex_re.match(lines[i]):
            for b in byte_re.findall(lines[i]):
                data.append(int(b, 16))
            i += 1
        output.append({
            'timestamp': ts,
            'data': base64.b64encode(data).decode('ascii')
        })

with open(os.path.join('go_client','testdata','draw_state.json'), 'w') as out:
    json.dump(output, out, indent=2)
