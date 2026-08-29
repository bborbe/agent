import os,re,collections
def fm(p):
    s=open(p,encoding="utf-8",errors="replace").read()
    if not s.startswith("---"): return {}
    end=s.find("\n---",3)
    if end<0: return {}
    d={}
    for line in s[3:end].splitlines():
        m=re.match(r'^([A-Za-z_][A-Za-z0-9_]*):\s*(.*)$',line)
        if m: d[m.group(1)]=m.group(2).strip().strip('"').strip("'")
    return d
for name,d in [("personal",os.path.expanduser("~/Documents/Obsidian/OpenClaw/tasks")),
               ("team",os.path.expanduser("~/Documents/Obsidian/OctopusAgent/tasks"))]:
    rows=[fm(os.path.join(d,f)) for f in os.listdir(d) if f.endswith(".md")]
    repos=set()
    for r in rows:
        m=re.search(r'github\.com/([^/]+/[^/.]+)',r.get("clone_url",""))
        if m: repos.add(m.group(1))
    done=sum(1 for r in rows if r.get("status")=="completed")
    ab=sum(1 for r in rows if r.get("status")=="aborted")
    print(f"{name}: {len(rows)} tasks, {len(repos)} repos, completed={done} ({done/len(rows)*100:.0f}%), aborted={ab} ({ab/len(rows)*100:.1f}%)")
    print("   repos:", sorted(repos)[:8], "...")
