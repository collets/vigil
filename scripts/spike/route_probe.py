#!/usr/bin/env python3
"""Observe local inference metadata or inject a provider error in a fresh fixture.

No payloads/credentials are logged. All profile routes are prepared against this
loopback relay. This is not an OS-level egress audit.
"""
import argparse
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import urllib.request
import urllib.error

ROOT=Path(__file__).resolve().parents[2]

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--live',action='store_true',required=True);p.add_argument('--error',action='store_true');a=p.parse_args()
    upstream=os.environ['OPENAI_BASE_URL'].removesuffix('/v1')
    if upstream!='http://127.0.0.1:8080': p.error('probe expects the pinned loopback server')
    key=os.environ['OPENAI_API_KEY'];observed=[]
    class Handler(BaseHTTPRequestHandler):
        def log_message(self,*args):pass
        def do_GET(self):self.forward()
        def do_POST(self):self.forward()
        def forward(self):
            n=int(self.headers.get('Content-Length','0'))
            if n>2**20:self.send_error(413);return
            body=self.rfile.read(n) if n else None
            model=None
            if body:
                try:model=json.loads(body).get('model')
                except (ValueError,AttributeError):self.send_error(400);return
            observed.append({'method':self.command,'path':self.path,'model':model})
            if a.error and self.command=='POST':
                self.send_response(400);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(b'{"error":{"message":"controlled fixture rejection","type":"invalid_request_error"}}');return
            if self.path not in ['/health','/props','/v1/models','/v1/chat/completions']:
                self.send_error(403);return
            request=urllib.request.Request(upstream+self.path,data=body,headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'},method=self.command)
            try:
                with urllib.request.urlopen(request,timeout=180) as response:
                    self.send_response(response.status);self.send_header('Content-Type',response.headers.get('Content-Type','application/json'));self.end_headers()
                    while True:
                        chunk=response.read1(8192)
                        if not chunk:break
                        self.wfile.write(chunk);self.wfile.flush()
            except (OSError,urllib.error.HTTPError):
                # Do not echo remote exception strings or authorization headers.
                self.close_connection=True
    server=ThreadingHTTPServer(('127.0.0.1',0),Handler);server.daemon_threads=True
    thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
    base=f'http://127.0.0.1:{server.server_port}/v1'
    try:
        prep=subprocess.run([sys.executable,str(ROOT/'scripts/spike/prepare.py'),'--base-url',base],cwd=ROOT,capture_output=True,text=True,check=True,timeout=40)
        folder=Path(prep.stdout.splitlines()[0].removeprefix('Prepared: '))
        env=dict(os.environ);env['OPENAI_BASE_URL']=base
        result=subprocess.run([str(ROOT/'bin/vigil'),'spike','--manifest',str(folder/'launch.json'),'--harness','hermes','--live'],cwd=ROOT,env=env,timeout=325)
        post=[r for r in observed if r['method']=='POST']
        evidence={'class':'installed gateway with controlled provider failure' if a.error else 'model-backed localhost recording relay','error_injected':a.error,'runner_exit':result.returncode,'requests':observed,'inference_requests':len(post),'unexpected_model':any(r['model']!='qwen3.8-27b-local' for r in post),'os_egress_enforced':False}
        dest=folder/'evidence/route-probe.json';dest.write_text(json.dumps(evidence,indent=2)+'\n');dest.chmod(0o600);print(dest)
        return int(not post or evidence['unexpected_model'] or ((result.returncode==0)==a.error))
    finally:server.shutdown();server.server_close();thread.join()

if __name__=='__main__':sys.exit(main())
