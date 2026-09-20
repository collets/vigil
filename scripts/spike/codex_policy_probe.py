#!/usr/bin/env python3
"""Exercise installed Codex command sandbox on a disposable fixture; no inference."""
import argparse
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import threading

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('manifest',type=Path);a=p.parse_args()
    manifest=a.manifest.resolve();m=json.loads(manifest.read_text());launch=m['codex'];work=Path(m['workspace'])
    bare=work/'remote.git'
    subprocess.run(['git','init','-q','--bare',str(bare)],env=launch['env'],check=True,timeout=10)
    child=subprocess.Popen(launch['argv'],cwd=work,env=launch['env'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,start_new_session=True)
    received=queue.Queue();counter=0
    def read():
        for line in child.stdout:
            if len(line)>1048576:received.put({'fatal':'oversize'});return
            try:received.put(json.loads(line))
            except ValueError:received.put({'fatal':'malformed'});return
    thread=threading.Thread(target=read,daemon=True);thread.start()
    def call(method,params):
        nonlocal counter
        counter+=1;child.stdin.write(json.dumps({'id':counter,'method':method,'params':params})+'\n');child.stdin.flush()
        while True:
            reply=received.get(timeout=20)
            if 'fatal' in reply:raise RuntimeError('invalid native frame')
            if reply.get('id')==counter and 'method' not in reply:return reply
            if 'method' in reply and 'id' in reply:
                child.stdin.write(json.dumps({'id':reply['id'],'error':{'code':-32800,'message':'fixture denies unexpected request'}})+'\n');child.stdin.flush()
    result={'class':'installed-native command/exec with explicit workspaceWrite policy; no model','cases':[],'hosting_actions':0}
    try:
        assert 'error' not in call('initialize',{'clientInfo':{'name':'vigil_native_probe','version':'0.1.0'}})
        child.stdin.write('{"method":"initialized","params":{}}\n');child.stdin.flush()
        commands=[('direct_commit',['git','-c','user.name=Fixture','-c','user.email=fixture@invalid','-c','commit.gpgsign=false','commit','--allow-empty','-m','native-policy-probe']),
                  ('python_ref_write',['python3','-c',"from pathlib import Path;Path('.git/refs/heads/probe').write_text(Path('.git/refs/heads/spike').read_text())"]),
                  ('local_push',['git','push',str(bare),'HEAD:refs/heads/probe'])]
        for name,command in commands:
            r=call('command/exec',{'command':command,'cwd':str(work),'timeoutMs':10000,'outputBytesCap':4096,'sandboxPolicy':{'type':'workspaceWrite','writableRoots':[str(work)],'networkAccess':False,'excludeTmpdirEnvVar':True,'excludeSlashTmp':True}})
            result['cases'].append({'case':name,'exit_code':r.get('result',{}).get('exitCode'),'rpc_error_code':r.get('error',{}).get('code')})
        def git(*args):
            r=subprocess.run(['git',*args],cwd=work,env=launch['env'],capture_output=True,text=True,timeout=10)
            return r.returncode,r.stdout.strip()
        result['head_unchanged']=git('rev-parse','HEAD')[1]==m['fixture_baseline']
        result['alternate_ref_created']=git('show-ref','--verify','--quiet','refs/heads/probe')[0]==0
        result['local_push_ref_created']=git('--git-dir',str(bare),'show-ref','--verify','--quiet','refs/heads/probe')[0]==0
        destination=manifest.parent/'evidence/codex-policy-probe.json';destination.write_text(json.dumps(result,indent=2)+'\n');destination.chmod(0o600);print(destination)
    finally:
        child.stdin.close()
        try:child.wait(timeout=5)
        except subprocess.TimeoutExpired:os.killpg(child.pid,signal.SIGKILL);child.wait(timeout=5)
        thread.join(timeout=1)

if __name__=='__main__':main()
