"""Run on the pilot via ssh. Return only synthetic values, schema and selected radio controls."""
import json
import re
from pathlib import Path

def capture(name):
    out=[]
    section=''
    for line in Path('/etc',name).read_text().splitlines(keepends=True):
        stripped=line.strip()
        if re.fullmatch(r'\[[A-Za-z0-9 -]+\]',stripped):
            section=stripped[1:-1]
            out.append(line)
        elif '=' in line and re.fullmatch(r'[A-Za-z0-9_]+',line.partition('=')[0].strip()):
            key,_,val=line.partition('=')
            value=val.strip()
            if section=='DMR' and key=='ColorCode' and re.fullmatch(r'\d{1,2}',value): pass
            elif section=='DMR Network 1' and key.startswith('TGRewrite') and re.fullmatch(r'\d+(,\d+){4}',value): pass
            elif key.strip() in ('Enable','Enabled','Duplex','Slot1','Slot2') and value in ('0','1'): pass
            elif key.strip()=='Callsign': value='N0DEMO'
            elif key.strip()=='Id': value='1234567'
            elif key.strip().endswith('Address'): value='127.0.0.1'
            elif key.strip()=='Port': value='62031'
            elif key.strip()=='Name': value='"QA station"'
            elif 'password' in key.lower(): value='qa-only'
            else: value='0'
            out.append(key+'='+value+'\n')
        elif stripped: out.append('; redacted site comment\n')
        else: out.append('\n')
    return ''.join(out)

print(json.dumps({p:capture(p) for p in ('MMDVM-Host.ini','DMRGateway.ini')}))
