package cbba

import (
	"encoding/json"
	"strings"
)

// ReportData is everything the replay page needs, embedded as JSON.
type ReportData struct {
	Tasks  []Task     `json:"tasks"`
	Robots []Robot    `json:"robots"`
	Params Params     `json:"params"`
	Rounds []RoundLog `json:"rounds"`
	Result Result     `json:"result"`
}

// RenderHTML produces a single self-contained HTML file (no external
// scripts, styles or services) that replays the auction round by round.
func RenderHTML(s *Sim, res Result) string {
	data := ReportData{
		Tasks:  s.Tasks,
		Robots: s.Robots,
		Params: s.Params,
		Rounds: s.Rounds,
		Result: res,
	}
	enc, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	// Guard against the JSON terminating the inline <script> element.
	enc = []byte(strings.ReplaceAll(string(enc), "</", "<\\/"))
	return strings.Replace(reportTemplate, "/*__DATA__*/", string(enc), 1)
}

// Note: the embedded page uses only string concatenation (no JS template
// literals) so the template stays a valid Go raw string literal.
const reportTemplate = `<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="utf-8">
<title>CBBA 拍卖共识回放</title>
<style>
  :root { --bg:#0f1420; --panel:#1a2132; --line:#2a3350; --txt:#dfe6f5;
          --dim:#8a94ad; --conflict:#ff5566; --resolve:#39d98a; --drop:#f0a832; }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--bg); color:var(--txt);
         font:14px/1.5 -apple-system,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif; }
  header { padding:14px 20px; border-bottom:1px solid var(--line); }
  header h1 { margin:0 0 6px; font-size:18px; }
  header .meta { color:var(--dim); font-size:13px; }
  header .meta b { color:var(--txt); }
  #controls { display:flex; gap:8px; align-items:center; padding:10px 20px;
              border-bottom:1px solid var(--line); position:sticky; top:0;
              background:var(--bg); z-index:5; }
  button { background:var(--panel); color:var(--txt); border:1px solid var(--line);
           border-radius:6px; padding:5px 12px; cursor:pointer; font-size:14px; }
  button:hover { border-color:var(--dim); }
  #roundLabel { min-width:150px; text-align:center; font-variant-numeric:tabular-nums; }
  input[type=range] { flex:1; accent-color:#5b8cff; }
  main { display:grid; grid-template-columns:minmax(320px,46%) 1fr; gap:14px; padding:14px 20px; }
  .panel { background:var(--panel); border:1px solid var(--line); border-radius:10px; padding:12px; }
  .panel h2 { margin:0 0 8px; font-size:14px; color:var(--dim); font-weight:600; }
  #map { position:relative; width:100%; aspect-ratio:3/2; background:#0b0f1a;
         border-radius:8px; overflow:hidden; }
  .dot { position:absolute; transform:translate(-50%,-50%); border-radius:50%;
         display:flex; align-items:center; justify-content:center; font-size:10px;
         font-weight:700; color:#0b0f1a; }
  .dot.robot { width:22px; height:22px; border:2px solid #fff3; z-index:2; }
  .dot.task { width:16px; height:16px; border-radius:4px; background:#39415c; color:#cfd6ea; }
  .dot.task.owned { color:#0b0f1a; }
  .dot.task.conflict { background:var(--conflict)!important; color:#fff;
         animation:blink .7s steps(2) infinite; }
  @keyframes blink { 50% { filter:brightness(1.8); } }
  .link { position:absolute; height:2px; transform-origin:0 50%; background:#5b8cff55; z-index:1; }
  #events { max-height:340px; overflow:auto; font-size:13px; }
  .ev { padding:3px 8px; border-left:3px solid var(--line); margin:3px 0; }
  .ev.bid { border-color:#5b8cff; }
  .ev.update { border-color:var(--resolve); }
  .ev.reset { border-color:#c586ff; }
  .ev.release { border-color:var(--resolve); background:#39d98a14; }
  .ev.drop { border-color:var(--drop); color:var(--drop); }
  #conflictBanner { display:none; padding:8px 12px; margin-bottom:8px; border-radius:8px;
         background:#ff556622; border:1px solid var(--conflict); color:#ffb3bb; }
  #robots { display:grid; grid-template-columns:repeat(auto-fill,minmax(190px,1fr)); gap:10px; }
  .rcard { background:#141a2a; border:1px solid var(--line); border-radius:8px; padding:8px; }
  .rcard .rname { font-weight:700; margin-bottom:6px; }
  .chip { display:inline-block; padding:1px 8px; margin:2px; border-radius:10px;
          font-size:12px; font-weight:600; color:#0b0f1a; }
  .chip.conflict { background:var(--conflict)!important; color:#fff; }
  .chip.new { outline:2px solid #fff8; }
  #final { margin:0 20px 20px; }
  #final .unassign { color:var(--drop); }
  .legend { font-size:12px; color:var(--dim); margin-top:8px; }
  .sw { display:inline-block; width:10px; height:10px; border-radius:3px; margin:0 4px 0 10px; }
</style>
</head>
<body>
<header>
  <h1>CBBA 多机器人任务分配 — 竞价与共识回放</h1>
  <div class="meta" id="meta"></div>
</header>
<div id="controls">
  <button id="first" title="第一轮">⏮</button>
  <button id="prev" title="上一轮">◀</button>
  <button id="play" title="播放/暂停">▶</button>
  <button id="next" title="下一轮">▶|</button>
  <button id="last" title="最后一轮">⏭</button>
  <input type="range" id="slider" min="0" max="0" value="0">
  <span id="roundLabel"></span>
</div>
<main>
  <div>
    <div class="panel">
      <h2>场景地图（任务颜色 = 当前归属机器人）</h2>
      <div id="map"></div>
      <div class="legend" id="legend"></div>
    </div>
  </div>
  <div>
    <div id="conflictBanner"></div>
    <div class="panel"><h2>本轮事件</h2><div id="events"></div></div>
    <div class="panel" style="margin-top:12px"><h2>机器人任务包（Bundle）</h2><div id="robots"></div></div>
  </div>
</main>
<div class="panel" id="final"></div>
<script>
const DATA = /*__DATA__*/;

const $ = id => document.getElementById(id);
const robotColor = i => 'hsl(' + ((i*67)%360) + ' 70% 62%)';

// ---- static layout -------------------------------------------------------
const xs = DATA.tasks.map(t=>t.x).concat(DATA.robots.map(r=>r.x));
const ys = DATA.tasks.map(t=>t.y).concat(DATA.robots.map(r=>r.y));
const minX=Math.min(...xs), maxX=Math.max(...xs), minY=Math.min(...ys), maxY=Math.max(...ys);
const nx = v => 6 + 88*(v-minX)/Math.max(1e-9,maxX-minX);
const ny = v => 6 + 88*(v-minY)/Math.max(1e-9,maxY-minY);

const mapEl = $('map');
DATA.robots.forEach(r=>{
  const d=document.createElement('div');
  d.className='dot robot'; d.style.left=nx(r.x)+'%'; d.style.top=ny(r.y)+'%';
  d.style.background=robotColor(r.id); d.textContent='R'+r.id;
  d.title='机器人 '+r.id+' @ ('+r.x.toFixed(1)+', '+r.y.toFixed(1)+')';
  mapEl.appendChild(d);
});
const taskDots = DATA.tasks.map(t=>{
  const d=document.createElement('div');
  d.className='dot task'; d.style.left=nx(t.x)+'%'; d.style.top=ny(t.y)+'%';
  d.textContent=t.id; d.title='任务 '+t.id+'：奖励 '+t.reward.toFixed(1);
  mapEl.appendChild(d); return d;
});
$('legend').innerHTML = DATA.robots.map(r=>
  '<span class="sw" style="background:'+robotColor(r.id)+'"></span>R'+r.id).join('') +
  '<span class="sw" style="background:var(--conflict)"></span>冲突';

// ---- meta / final summary ------------------------------------------------
$('meta').innerHTML =
  '机器人 <b>'+DATA.robots.length+'</b> · 任务 <b>'+DATA.tasks.length+'</b> · ' +
  '总轮数 <b>'+DATA.result.rounds+'</b> · 收敛 <b>'+(DATA.result.converged?'是':'否（达到轮数上限）')+'</b> · ' +
  '已分配 <b>'+Object.keys(DATA.result.assignment).length+'</b> · ' +
  '无法承接 <b>'+DATA.result.unassignable.length+'</b>';

{
  const lines = [];
  lines.push('<h2>最终分配</h2>');
  const byRobot = {};
  DATA.robots.forEach(r=>byRobot[r.id]=[]);
  Object.entries(DATA.result.assignment).forEach(kv=>byRobot[kv[1]].push(+kv[0]));
  DATA.robots.forEach(r=>{
    const ts = byRobot[r.id].sort((a,b)=>a-b);
    lines.push('<div>机器人 R'+r.id+'：'+(ts.length?ts.map(t=>
      '<span class="chip" style="background:'+robotColor(r.id)+'">T'+t+'</span>').join('')
      :'<span style="color:var(--dim)">（无任务）</span>')+'</div>');
  });
  if (DATA.result.unassignable.length){
    lines.push('<h2 style="margin-top:10px">无法承接的任务（显式报告，未静默丢弃）</h2>');
    DATA.result.unassignable.forEach(u=>{
      lines.push('<div class="unassign">⚠ 任务 T'+u.taskId+'：'+u.reason+'</div>');
    });
  }
  if (DATA.result.conflicts.length){
    lines.push('<h2 style="color:var(--conflict)">未解决冲突</h2>');
    DATA.result.conflicts.forEach(c=>
      lines.push('<div style="color:var(--conflict)">任务 T'+c.task+' 被机器人 '+c.robots.join(', ')+' 同时持有</div>'));
  }
  $('final').innerHTML = lines.join('');
}

// ---- round replay --------------------------------------------------------
let round = 0, playing = false, timer = null;
$('slider').max = DATA.rounds.length-1;

function render(){
  const R = DATA.rounds[round];
  $('roundLabel').textContent = '第 '+R.number+' / '+DATA.rounds.length+' 轮';
  $('slider').value = round;

  // ownership map from bundles
  const owners = {};
  R.bundles.forEach((bundle, rid)=>bundle.forEach(t=>owners[t]=rid));
  const conflictTasks = new Set(R.conflicts.map(c=>c.task));

  // map: task ownership colors + conflict highlight
  DATA.tasks.forEach((t,i)=>{
    const d = taskDots[i];
    const owner = owners[t.id];
    const inConflict = conflictTasks.has(t.id);
    d.classList.toggle('conflict', inConflict);
    d.classList.toggle('owned', owner!==undefined && !inConflict);
    d.style.background = inConflict ? '' :
      (owner!==undefined ? robotColor(owner) : '#39415c');
  });
  // map: links robot -> its tasks
  mapEl.querySelectorAll('.link').forEach(e=>e.remove());
  R.bundles.forEach((bundle, rid)=>{
    const r = DATA.robots[rid];
    bundle.forEach(tid=>{
      const t = DATA.tasks[tid];
      const x1=nx(r.x), y1=ny(r.y), x2=nx(t.x), y2=ny(t.y);
      const len = Math.hypot(x2-x1,y2-y1), ang = Math.atan2(y2-y1,x2-x1);
      const l = document.createElement('div');
      l.className='link';
      l.style.left=x1+'%'; l.style.top=y1+'%';
      l.style.width=len+'%'; l.style.transform='rotate('+ang+'rad)';
      l.style.background = conflictTasks.has(tid) ? 'var(--conflict)' : robotColor(rid)+'66';
      mapEl.appendChild(l);
    });
  });

  // conflict banner
  const banner = $('conflictBanner');
  if (R.conflicts.length){
    banner.style.display='block';
    banner.innerHTML = '⚠ 本轮冲突：' + R.conflicts.map(c=>
      '任务 T'+c.task+'（机器人 '+c.robots.map(r=>'R'+r).join(' 与 ')+'）').join('；');
  } else banner.style.display='none';

  // events
  const evEl = $('events');
  if (!R.events.length) evEl.innerHTML = '<div style="color:var(--dim)">本轮无事件（静默轮，用于确认收敛）</div>';
  else evEl.innerHTML = R.events.map(e=>{
    let txt='';
    switch(e.kind){
      case 'bid':     txt='R'+e.robot+' 对 T'+e.task+' 出价 '+e.bid.toFixed(3)+'（插入路径位置 '+e.pos+'）'; break;
      case 'update':  txt='R'+e.robot+' 接受 R'+e.from+' 的信息：T'+e.task+' 赢家 '+(e.oldWinner<0?'无':'R'+e.oldWinner)+' → R'+e.newWinner; break;
      case 'reset':   txt='R'+e.robot+' 依据 R'+e.from+' 的信息重置 T'+e.task+'（原赢家 R'+e.oldWinner+' 信息过期）'; break;
      case 'release': txt='R'+e.robot+' 释放 T'+e.task+'（被更高出价击败，级联释放）'; break;
      case 'drop':    txt='✉✗ R'+e.from+' → R'+e.to+' 的消息丢失'; break;
    }
    return '<div class="ev '+e.kind+'">'+txt+'</div>';
  }).join('');

  // robot bundle cards
  const newBids = new Set();
  R.events.filter(e=>e.kind==='bid').forEach(e=>newBids.add(e.robot+':'+e.task));
  $('robots').innerHTML = DATA.robots.map(r=>{
    const bundle = R.bundles[r.id]||[];
    const chips = bundle.map(t=>{
      const conf = conflictTasks.has(t);
      return '<span class="chip'+(conf?' conflict':'')+(newBids.has(r.id+':'+t)?' new':'')+'"'+
        ' style="background:'+(conf?'':robotColor(r.id))+'">T'+t+'</span>';
    }).join('');
    return '<div class="rcard" style="border-top:3px solid '+robotColor(r.id)+'">'+
      '<div class="rname">R'+r.id+'</div>'+
      (bundle.length ? chips : '<span style="color:var(--dim)">（空）</span>')+'</div>';
  }).join('');
}

function step(d){ round = Math.min(DATA.rounds.length-1, Math.max(0, round+d)); render(); }
$('first').onclick = ()=>{ round=0; render(); };
$('prev').onclick  = ()=>step(-1);
$('next').onclick  = ()=>step(1);
$('last').onclick  = ()=>{ round=DATA.rounds.length-1; render(); };
$('slider').oninput = e=>{ round=+e.target.value; render(); };
$('play').onclick = ()=>{
  playing = !playing;
  $('play').textContent = playing ? '⏸' : '▶';
  if (playing){
    timer = setInterval(()=>{
      if (round >= DATA.rounds.length-1){ $('play').onclick(); return; }
      step(1);
    }, 1200);
  } else clearInterval(timer);
};
document.addEventListener('keydown', e=>{
  if (e.key==='ArrowLeft') step(-1);
  if (e.key==='ArrowRight') step(1);
  if (e.key===' ') { e.preventDefault(); $('play').onclick(); }
});
render();
</script>
</body>
</html>`
