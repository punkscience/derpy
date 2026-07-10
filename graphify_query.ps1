$py = (Get-Content graphify-out\.graphify_python).Trim()
& $py -c @"
import sys, json
from networkx.readwrite import json_graph
import networkx as nx
from pathlib import Path

data = json.loads(Path('graphify-out/graph.json').read_text())
G = json_graph.node_link_graph(data, edges='links')

question = 'new player model bluesky tag sum cache audio playback tui config'
mode = 'bfs'
terms = [t.lower() for t in question.split() if len(t) >= 3]

scored = []
for nid, ndata in G.nodes(data=True):
    label = ndata.get('label', '').lower()
    score = sum(1 for t in terms if t in label)
    if score > 0:
        scored.append((score, nid))
scored.sort(reverse=True)
start_nodes = [nid for _, nid in scored[:3]]

if not start_nodes:
    print('No matching nodes found for query terms:', terms)
    sys.exit(0)

subgraph_nodes = set()
subgraph_edges = []

frontier = set(start_nodes)
subgraph_nodes = set(start_nodes)
for _ in range(3):
    next_frontier = set()
    for n in frontier:
        for neighbor in G.neighbors(n):
            if neighbor not in subgraph_nodes:
                next_frontier.add(neighbor)
                subgraph_edges.append((n, neighbor))
    subgraph_nodes.update(next_frontier)
    frontier = next_frontier

token_budget = 3000
char_budget = token_budget * 4

def relevance(nid):
    label = G.nodes[nid].get('label', '').lower()
    return sum(1 for t in terms if t in label)

ranked_nodes = sorted(subgraph_nodes, key=relevance, reverse=True)

lines = [f'Traversal: BFS | Start: {[G.nodes[n].get(\"label\",n) for n in start_nodes]} | {len(subgraph_nodes)} nodes']
for nid in ranked_nodes[:60]:
    d = G.nodes[nid]
    lines.append(f'  NODE {d.get(\"label\", nid)} [src={d.get(\"source_file\",\"\")} loc={d.get(\"source_location\",\"\")}] [community={d.get(\"community\",\"\")}]')
for u, v in subgraph_edges:
    if u in subgraph_nodes and v in subgraph_nodes:
        _raw = G[u][v]; d = next(iter(_raw.values()), {}) if isinstance(G, nx.MultiGraph) else _raw
        u_label = G.nodes[u].get('label',u)
        v_label = G.nodes[v].get('label',v)
        rel = d.get('relation','')
        conf = d.get('confidence','')
        lines.append(f'  EDGE {u_label} --{rel} [{conf}]--> {v_label}')

output = '\n'.join(lines)
if len(output) > char_budget:
    output = output[:char_budget] + '\n... (truncated)'
print(output)
"@
