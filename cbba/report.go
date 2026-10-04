package cbba

import (
	"encoding/json"
	"fmt"
	"io"
)

// reportData is the JSON payload embedded into the static HTML report.
type reportData struct {
	Robots       []Robot       `json:"robots"`
	Tasks        []Task        `json:"tasks"`
	Lambda       float64       `json:"lambda"`
	Converged    bool          `json:"converged"`
	Rounds       int           `json:"rounds"`
	Assignment   map[int][]int `json:"assignment"`
	Unassignable []int         `json:"unassignable"`
	Unassigned   []int         `json:"unassigned"`
	Conflicts    []Conflict    `json:"conflicts"`
	Log          []RoundLog    `json:"log"`
}

// WriteReport renders the full replay of a run as a single self-contained
// static HTML file (no server, no external assets, no frameworks).
func WriteReport(w io.Writer, cfg Config, res *Result) error {
	data := reportData{
		Robots:       cfg.Robots,
		Tasks:        cfg.Tasks,
		Lambda:       cfg.Lambda,
		Converged:    res.Converged,
		Rounds:       res.Rounds,
		Assignment:   res.Assignment,
		Unassignable: res.Unassignable,
		Unassigned:   res.Unassigned,
		Conflicts:    res.Conflicts,
		Log:          res.Log,
	}
	if data.Unassignable == nil {
		data.Unassignable = []int{}
	}
	if data.Unassigned == nil {
		data.Unassigned = []int{}
	}
	if data.Conflicts == nil {
		data.Conflicts = []Conflict{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal report data: %w", err)
	}
	for _, part := range []string{reportHTML1, string(raw), reportHTML2} {
		if _, err := io.WriteString(w, part); err != nil {
			return err
		}
	}
	return nil
}

const reportHTML1 = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>CBBA 多机器人任务分配 · 逐轮回放</title>
<style>
:root{
  --bg:#0f1420;--panel:#171e2e;--panel2:#1e2740;--line:#2b3755;
  --text:#e6ebf5;--muted:#93a0bd;--accent:#5b8cff;
  --ok:#34c98b;--warn:#f5a623;--bad:#f2545b;--drop:#e06c75;
}
*{box-sizing:border-box}
html,body{margin:0;padding:0;background:var(--bg);color:var(--text);
  font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif}
header{padding:14px 20px;border-bottom:1px solid var(--line);display:flex;flex-wrap:wrap;gap:10px;align-items:center}
header h1{font-size:17px;margin:0;font-weight:650}
.badge{display:inline-block;padding:2px 10px;border-radius:999px;font-size:12px;font-weight:600}
.badge.ok{background:rgba(52,201,139,.15);color:var(--ok)}
.badge.bad{background:rgba(242,84,91,.15);color:var(--bad)}
.badge.warn{background:rgba(245,166,35,.15);color:var(--warn)}
.badge.neutral{background:var(--panel2);color:var(--muted)}
main{display:grid;grid-template-columns:minmax(0,1fr) 400px;gap:14px;padding:14px 20px}
@media(max-width:980px){main{grid-template-columns:1fr}}
.panel{background:var(--panel);border:1px solid var(--line);border-radius:10px;padding:12px}
.panel h2{font-size:13px;margin:0 0 8px;color:var(--muted);text-transform:uppercase;letter-spacing:.06em}
#controls{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-bottom:10px}
button{background:var(--panel2);color:var(--text);border:1px solid var(--line);border-radius:8px;
  padding:5px 14px;cursor:pointer;font-size:13px}
button:hover{border-color:var(--accent)}
input[type=range]{flex:1;min-width:120px;accent-color:var(--accent)}
#roundLabel{font-variant-numeric:tabular-nums;color:var(--muted);min-width:150px}
svg{width:100%;height:auto;display:block;background:var(--panel2);border-radius:8px}
.side{display:flex;flex-direction:column;gap:14px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:4px 8px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}
th{color:var(--muted);font-weight:600}
.chip{display:inline-block;background:var(--panel2);border-radius:6px;padding:1px 7px;margin:1px 2px;font-size:12px}
.chip.conflict{background:rgba(242,84,91,.25);color:#ffb3b6}
.chip.won{background:rgba(52,201,139,.18);color:var(--ok)}
.chip.unassignable{background:rgba(245,166,35,.18);color:var(--warn)}
.ev-bid{color:var(--accent)}
.ev-resolve{color:var(--ok)}
.ev-release,.ev-reset{color:var(--warn)}
.ev-conflict{color:var(--bad);font-weight:600}
.ev-drop{color:var(--drop)}
#events{max-height:260px;overflow:auto;font-size:13px}
#events div{padding:2px 4px;border-bottom:1px solid var(--line)}
.legend{font-size:12px;color:var(--muted);margin-top:8px}
.legend span{margin-right:10px}
.dot{display:inline-block;width:9px;height:9px;border-radius:50%;margin-right:4px}
</style>
</head>
<body>
<header>
  <h1>CBBA 多机器人任务分配 · 逐轮回放</h1>
  <span id="convBadge"></span>
  <span id="statBadges"></span>
</header>
<main>
  <section class="panel">
    <h2>竞价与共识过程</h2>
    <div id="controls">
      <button id="prevBtn">⟨ 上一轮</button>
      <button id="playBtn">播放</button>
      <button id="nextBtn">下一轮 ⟩</button>
      <input id="slider" type="range" min="0" value="0">
      <span id="roundLabel"></span>
    </div>
    <svg id="map" viewBox="0 0 600 600" role="img" aria-label="机器人与任务分布图"></svg>
    <div class="legend">
      <span><span class="dot" style="background:var(--accent)"></span>机器人</span>
      <span><span class="dot" style="background:#8b96b3"></span>未分配任务</span>
      <span><span class="dot" style="background:var(--ok)"></span>已达成共识</span>
      <span><span class="dot" style="background:var(--bad)"></span>冲突（多方认领）</span>
      <span><span class="dot" style="background:var(--warn)"></span>无法承接</span>
    </div>
  </section>
  <div class="side">
    <section class="panel">
      <h2>本轮事件</h2>
      <div id="events"></div>
    </section>
    <section class="panel">
      <h2>机器人任务包（本轮结束时的信念）</h2>
      <table id="bundles"><thead><tr><th>机器人</th><th>任务包（有序）</th></tr></thead><tbody></tbody></table>
    </section>
    <section class="panel">
      <h2>最终结果</h2>
      <div id="final"></div>
    </section>
  </div>
</main>
<script>
"use strict";
const DATA = `

const reportHTML2 = `;

const NS = "http://www.w3.org/2000/svg";
const robotColor = id => "hsl(" + ((id * 67) % 360) + ",70%,60%)";
const $ = id => document.getElementById(id);

function taskById(id){ return DATA.tasks.find(t => t.id === id); }
function robotById(id){ return DATA.robots.find(r => r.id === id); }

// --- header badges -------------------------------------------------------
$("convBadge").innerHTML = DATA.converged
  ? '<span class="badge ok">已收敛 · ' + DATA.rounds + ' 轮</span>'
  : '<span class="badge bad">未收敛（达到轮次上限 ' + DATA.rounds + '）</span>';
let badges = "";
badges += '<span class="badge neutral">机器人 ' + DATA.robots.length + '</span> ';
badges += '<span class="badge neutral">任务 ' + DATA.tasks.length + '</span> ';
if (DATA.conflicts.length)
  badges += '<span class="badge bad">最终冲突 ' + DATA.conflicts.length + '</span> ';
else
  badges += '<span class="badge ok">无冲突</span> ';
if (DATA.unassignable.length)
  badges += '<span class="badge warn">无法承接任务 ' + DATA.unassignable.length + ' 个</span> ';
if (DATA.unassigned.length)
  badges += '<span class="badge warn">容量原因未分配 ' + DATA.unassigned.length + ' 个</span> ';
$("statBadges").innerHTML = badges;

// --- final panel ---------------------------------------------------------
(function(){
  let h = "<table><thead><tr><th>机器人</th><th>最终任务</th></tr></thead><tbody>";
  for (const r of DATA.robots){
    const tasks = (DATA.assignment[r.id] || []).map(id =>
      '<span class="chip won">T' + id + "</span>").join(" ");
    h += "<tr><td>R" + r.id + "</td><td>" + (tasks || "—") + "</td></tr>";
  }
  h += "</tbody></table>";
  if (DATA.unassignable.length)
    h += '<p>无法承接（任何机器人都无法以正收益接受）：' +
      DATA.unassignable.map(id => '<span class="chip unassignable">T' + id + "</span>").join(" ") + "</p>";
  if (DATA.unassigned.length)
    h += '<p>可承接但未分配（容量/竞争原因）：' +
      DATA.unassigned.map(id => '<span class="chip">T' + id + "</span>").join(" ") + "</p>";
  if (DATA.conflicts.length)
    h += '<p class="ev-conflict">最终冲突：' + DATA.conflicts.map(c =>
      "T" + c.task + " ← R" + c.robots.join(", R")).join("；") + "</p>";
  $("final").innerHTML = h;
})();

// --- map -----------------------------------------------------------------
const PADDING = 40, SIZE = 600;
function scales(){
  const xs = [], ys = [];
  for (const r of DATA.robots){ xs.push(r.x); ys.push(r.y); }
  for (const t of DATA.tasks){ xs.push(t.x); ys.push(t.y); }
  const minX = Math.min(...xs), maxX = Math.max(...xs);
  const minY = Math.min(...ys), maxY = Math.max(...ys);
  const sx = v => PADDING + (maxX === minX ? SIZE/2 - PADDING :
    (v - minX) / (maxX - minX) * (SIZE - 2*PADDING));
  const sy = v => PADDING + (maxY === minY ? SIZE/2 - PADDING :
    (v - minY) / (maxY - minY) * (SIZE - 2*PADDING));
  return [sx, sy];
}
const [SX, SY] = scales();

function el(name, attrs){
  const e = document.createElementNS(NS, name);
  for (const k in attrs) e.setAttribute(k, attrs[k]);
  return e;
}

function renderMap(roundIdx){
  const svg = $("map");
  svg.innerHTML = "";
  const log = DATA.log[roundIdx];
  // task -> set of robots believing they own it (this round)
  const claims = {};
  for (const st of log.states)
    for (const tid in st.winners)
      if (st.winners[tid] === st.id){
        (claims[tid] = claims[tid] || new Set()).add(st.id);
      }
  // edges: robot -> each bundle task
  for (const st of log.states){
    const r = robotById(st.id);
    let px = SX(r.x), py = SY(r.y);
    for (const tid of st.bundle){
      const t = taskById(tid);
      svg.appendChild(el("line", {x1:px, y1:py, x2:SX(t.x), y2:SY(t.y),
        stroke: robotColor(st.id), "stroke-width":1.2, opacity:.45}));
      px = SX(t.x); py = SY(t.y);
    }
  }
  // tasks
  for (const t of DATA.tasks){
    const owners = claims[t.id];
    let fill = "#8b96b3", stroke = "#dfe7ff", extra = {};
    if (DATA.unassignable.includes(t.id)){ fill = "var(--warn)"; }
    if (owners && owners.size === 1){ fill = robotColor([...owners][0]); }
    if (owners && owners.size > 1){
      fill = "var(--bad)"; stroke = "#ffd2d4";
      extra = {"stroke-width": 2.5};
    }
    const c = el("rect", Object.assign({
      x: SX(t.x)-6, y: SY(t.y)-6, width:12, height:12, rx:2,
      fill, stroke, "stroke-width":1}, extra));
    c.appendChild(el("title", {})).textContent =
      "T" + t.id + (owners ? " 认领: R" + [...owners].join(",R") : " 未认领");
    svg.appendChild(c);
    const lbl = el("text", {x:SX(t.x), y:SY(t.y)+3, "text-anchor":"middle",
      "font-size":8, "font-weight":700, fill:"#0b1020"});
    lbl.textContent = t.id;
    svg.appendChild(lbl);
  }
  // robots
  for (const r of DATA.robots){
    svg.appendChild(el("circle", {cx:SX(r.x), cy:SY(r.y), r:8,
      fill:robotColor(r.id), stroke:"#dfe7ff", "stroke-width":1.2}));
    const lbl = el("text", {x:SX(r.x), y:SY(r.y)+3.5, "text-anchor":"middle",
      "font-size":9, "font-weight":700, fill:"#0b1020"});
    lbl.textContent = "R" + r.id;
    svg.appendChild(lbl);
  }
}

// --- events & bundles ----------------------------------------------------
const EV_LABEL = {bid:"出价", resolve:"让位", release:"移除", reset:"重置",
  conflict:"冲突", drop:"丢包"};
function describe(ev){
  const R = id => "R" + id, T = id => "T" + id;
  switch (ev.type){
    case "bid": return R(ev.robot) + " 对 " + T(ev.task) + " 出价 " +
      ev.bid.toFixed(2) + "（" + ev.note + "）";
    case "resolve": return R(ev.robot) + " 在 " + T(ev.task) + " 上让位给 " + R(ev.other);
    case "release": return R(ev.robot) + " 从任务包移除 " + T(ev.task);
    case "reset": return R(ev.robot) + " 接受 " + R(ev.other) + " 对 " + T(ev.task) + " 的释放";
    case "conflict": return R(ev.robot) + " 与 " + R(ev.other) + " 同时认领 " + T(ev.task);
    case "drop": return R(ev.robot) + " → " + R(ev.other) + " 的共识消息丢失";
  }
  return "";
}
function renderRound(i){
  const log = DATA.log[i];
  $("roundLabel").textContent = "第 " + (i+1) + " / " + DATA.log.length + " 轮";
  $("slider").value = i;
  renderMap(i);
  const evBox = $("events");
  evBox.innerHTML = log.events.length
    ? log.events.map(e =>
        '<div class="ev-' + e.type + '">[' + EV_LABEL[e.type] + "] " +
        describe(e) + "</div>").join("")
    : '<div style="color:var(--muted)">本轮无事件（状态稳定）</div>';
  const tb = $("bundles").querySelector("tbody");
  tb.innerHTML = log.states.map(st => {
    const chips = st.bundle.map(tid => {
      const conflicted = log.events.some(e => e.type === "conflict" && e.task === tid);
      return '<span class="chip' + (conflicted ? " conflict" : "") + '">T' + tid + "</span>";
    }).join(" ");
    return "<tr><td>R" + st.id + "</td><td>" + (chips || "—") + "</td></tr>";
  }).join("");
}

// --- controls ------------------------------------------------------------
let cur = 0, timer = null;
function stop(){ if (timer){ clearInterval(timer); timer = null; $("playBtn").textContent = "播放"; } }
$("prevBtn").onclick = () => { stop(); if (cur > 0) renderRound(--cur); };
$("nextBtn").onclick = () => { stop(); if (cur < DATA.log.length-1) renderRound(++cur); };
$("slider").max = DATA.log.length - 1;
$("slider").oninput = e => { stop(); cur = +e.target.value; renderRound(cur); };
$("playBtn").onclick = () => {
  if (timer){ stop(); return; }
  $("playBtn").textContent = "暂停";
  timer = setInterval(() => {
    if (cur >= DATA.log.length - 1){ stop(); return; }
    renderRound(++cur);
  }, 900);
};
renderRound(0);
</script>
</body>
</html>
`
