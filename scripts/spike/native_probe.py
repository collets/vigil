#!/usr/bin/env python3
"""Pinned Hermes native request/terminal probes, without model inference.

Run with a fresh prepared manifest. Stimuli are injected at native module entry
points, not synthesized model decisions. Only disposable Git repositories change.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import shlex
import subprocess
import sys
import threading

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument("manifest",type=Path)
    p.add_argument("--child",action="store_true")
    a=p.parse_args(); manifest=a.manifest.resolve()
    m=json.loads(manifest.read_text()); launch=m['hermes']; work=Path(m['workspace'])
    if not a.child:
        env=dict(launch['env']);env['HERMES_INTERACTIVE']='1'
        result=subprocess.run([launch['argv'][0],str(Path(__file__).resolve()),str(manifest),'--child'],env=env,cwd=work,capture_output=True,text=True,timeout=90)
        if result.returncode: print('Native probe failed; no native stderr copied');return result.returncode
        print(result.stdout.strip());return 0
    from tui_gateway import server_requests as sr
    from tools import terminal_tool as terminal
    from tools import approval_context
    source=Path(launch['env']['PYTHONPATH'])
    evidence={'class':'installed-native integration; controlled stimuli; no inference','requests':[], 'policy':[], 'source_sha256':{}}
    for name in ['tui_gateway/server_requests.py','tools/approval.py','tools/terminal_tool.py']:
        evidence['source_sha256'][name]=hashlib.sha256((source/name).read_bytes()).hexdigest()
    frames=queue.Queue();cancelled=[]
    sr.bind_sinks(frames.put,lambda kind,sid,payload:cancelled.append((kind,payload['reason'])),lambda sid:True)
    for method,decision in [('approval','once'),('approval','deny'),('clarify','answer'),('clarify','cancel'),('approval','timeout'),('clarify','interrupt')]:
        params={'request_id':'fixture','command':'printf fixture','choices':['once','deny']} if method=='approval' else {'question':'Choose a color','choices':['blue','green']}
        box=[]
        thread=threading.Thread(target=lambda:box.append(sr.send(method,'fixture-session',params,timeout=.1 if decision=='timeout' else 2)))
        thread.start();frame=frames.get(timeout=2)
        if decision=='interrupt': sr.cancel('fixture-session','interrupted')
        elif decision!='timeout':
            response={'choice':decision} if method=='approval' else ({'answer':'blue'} if decision=='answer' else {})
            assert sr.resolve_response({'id':frame['id'],'result':response})
        thread.join(3);assert not thread.is_alive()
        late=sr.resolve_response({'id':frame['id'],'result':{'choice':'once'}})
        assert not late and sr.open_request_count()==0
        evidence['requests'].append({'method':method,'decision':decision,'returned':box[0],'late_response_rejected':not late})
    evidence['native_cancellations']=cancelled
    approvals=[]
    def deny(*args,**kwargs): approvals.append(True);return 'deny'
    terminal.set_approval_callback(deny)
    evidence['approval_mode']=approval_context._get_approval_mode()
    assert evidence['approval_mode']=='manual'
    repo=work/'policy-repo';bare=work/'policy-remote.git'
    def git(*args,cwd=work): return subprocess.run(['git',*args],cwd=cwd,check=True,capture_output=True,text=True,timeout=10).stdout.strip()
    git('init','-q',str(repo));git('init','-q','--bare',str(bare))
    for key,value in [('user.name','Vigil Fixture'),('user.email','fixture@invalid'),('commit.gpgsign','false')]:git('config',key,value,cwd=repo)
    cmds=[('direct_commit','git commit --allow-empty -m direct-fixture'),
          ('python_commit',shlex.quote(sys.executable)+' -c '+shlex.quote("import subprocess;subprocess.run(['git','commit','--allow-empty','-m','python-fixture'],check=True)")),
          ('local_push','git push '+shlex.quote(str(bare))+' HEAD:refs/heads/probe')]
    for label,command in cmds:
        before=len(approvals)
        response=json.loads(terminal.terminal_tool(command,workdir=str(repo),timeout=10,task_id='vigil-policy-probe'))
        evidence['policy'].append({'case':label,'exit_code':response.get('exit_code'),'native_approval_requests':len(approvals)-before,'error_present':bool(response.get('error'))})
    count=git('rev-list','--count','HEAD',cwd=repo)
    evidence['actual_commit_count']=int(count)
    evidence['local_push_ref']=git('--git-dir',str(bare),'rev-parse','refs/heads/probe')
    evidence['hosting_actions']=0
    evidence['hard_application_commit_push_gate']='unsupported in unrestricted Hermes terminal profile' if int(count)>0 else 'inconclusive'
    destination=manifest.parent/'evidence/native-probe.json';destination.write_text(json.dumps(evidence,indent=2)+'\n');destination.chmod(0o600)
    print(destination)
    return 0

if __name__=='__main__':sys.exit(main())
