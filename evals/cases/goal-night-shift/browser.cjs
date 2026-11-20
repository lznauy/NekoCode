const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const {chromium} = require('playwright-core');
const {level, move, won, pathTo, directions} = require('./rules.cjs');
const maps = require('./fixture/levels.json').map(spec => level(spec.rows));
const [url,output] = process.argv.slice(2);
const keys = {up:'ArrowUp', down:'ArrowDown', left:'ArrowLeft', right:'ArrowRight'};
const checks = [], faults = new Set();

async function match(page, n, state, phase='playing') {
  const map = maps[n-1];
  const expected = {n:String(n),phase,moves:String(state.moves),pushes:String(state.pushes),
    cells:map.tiles.map((t,i) => [t, state.player===i?'player':state.boxes.includes(i)?'crate':'none'])};
  await page.waitForFunction(({expected,width}) => {
    const board = document.querySelector('#board');
    if (!board) return false;
    const cells = [];
    for (const el of board.querySelectorAll('[data-x][data-y]')) {
      const i = Number(el.dataset.y)*width + Number(el.dataset.x);
      if (cells[i] || +el.dataset.x<0 || +el.dataset.x>=width || +el.dataset.y<0) return false;
      cells[i] = [el.dataset.tile,el.dataset.piece];
    }
    const actual = {n:board.dataset.level,phase:board.dataset.phase,
      moves:document.querySelector('#moves')?.textContent.trim(),
      pushes:document.querySelector('#pushes')?.textContent.trim(),cells};
    return JSON.stringify(actual)===JSON.stringify(expected);
  }, {expected,width:map.width}, {timeout:1600}).catch(async error=>{
    const actual=await page.evaluate(()=>({
      level:document.querySelector('#board')?.dataset.level,
      phase:document.querySelector('#board')?.dataset.phase,
      moves:document.querySelector('#moves')?.textContent.trim(),
      pushes:document.querySelector('#pushes')?.textContent.trim(),
      player:document.querySelector('[data-piece="player"]')?.getAttribute('data-x')+','+
        document.querySelector('[data-piece="player"]')?.getAttribute('data-y')
    }));
    throw Error(`Expected level ${n} ${phase}, moves=${state.moves}, pushes=${state.pushes}, player cell=${state.player}; observed ${JSON.stringify(actual)} (${error.name})`);
  });
}

async function walk(page,n,state,sequence) {
  for (const direction of sequence) {
    await page.keyboard.press(keys[direction]);
    state = move(maps[n-1],state,direction);
    await match(page,n,state,won(maps[n-1],state)?(n===3?'complete':'won'):'playing');
  }
  return state;
}
const solve = (p,n,s) => walk(p,n,s,pathTo(maps[n-1],s,x=>won(maps[n-1],x)));
const click = (p,id) => p.locator('#'+id).click();

(async()=>{
  const browser = await chromium.launch({executablePath:process.env.CHROME_BIN, args:['--no-sandbox']});
  try {
    async function scenario(id,fn,mobile=false) {
      const context = await browser.newContext({viewport:mobile?{width:360,height:800}:{width:1280,height:800},hasTouch:mobile,
        reducedMotion:'reduce',serviceWorkers:'block'});
      const page = await context.newPage();
      page.setDefaultTimeout(1800);
      page.on('pageerror',e=>faults.add(e.message));
      page.on('response',r=>{if(r.status()>=400) faults.add('HTTP '+r.status()+' '+r.url())});
      await context.route('**/*',async r=>{
        const u=new URL(r.request().url());
        if (u.origin===new URL(url).origin || ['data:','blob:'].includes(u.protocol)) return r.continue();
        faults.add('External request: '+u.origin); return r.abort();
      });
      try {
        await page.goto(url);
        await match(page,1,maps[0].start);
        await fn(page);
        checks.push({id,status:'passed',evidence:'Verified in Chromium: '+id});
      } catch(e) {
        checks.push({id,status:'failed',evidence:String(e).slice(0,1800)});
      } finally {await context.close();}
    }
    const initial=maps[0].start;
    await scenario('movement',async p=>{
      await match(p,1,initial);
      await p.screenshot({path:path.join(output,'desktop.png')});
      // Wall collision, walk back, push, then undo the push and a walk.
      let s=await walk(p,1,initial,['left','left','right','right']);
      assert.equal(s.pushes,1);
      await click(p,'undo'); await match(p,1,{...initial,moves:2});
      await click(p,'undo'); await match(p,1,move(maps[0],initial,'left'));
      await click(p,'restart'); await match(p,1,initial);
      await solve(p,1,initial); await click(p,'next');
      const map=maps[1];
      const blockedDirection=s=>Object.keys(directions).find(d=>{
        const [dx,dy]=directions[d],step=dx+dy*map.width;
        return s.boxes.includes(s.player+step) && s.boxes.includes(s.player+2*step);
      });
      s=await walk(p,2,map.start,pathTo(map,map.start,blockedDirection));
      await walk(p,2,s,[blockedDirection(s)]);
    });
    await scenario('resume',async p=>{
      const s=await walk(p,1,initial,['right']);
      await click(p,'pause'); await match(p,1,s,'paused');
      await p.keyboard.press('ArrowRight'); await match(p,1,s,'paused');
      await click(p,'pause'); await match(p,1,s);
      await p.reload(); await match(p,1,s);
      await click(p,'undo'); await match(p,1,initial);
    });
    await scenario('campaign',async p=>{
      for(let n=1;n<=maps.length;n++){
        const start=maps[n-1].start;
        await match(p,n,start);
        const end=await solve(p,n,start);
        await p.keyboard.press('ArrowLeft'); await match(p,n,end,n===3?'complete':'won');
        if (n<3) await click(p,'next');
      }
      await click(p,'replay'); await match(p,1,initial);
    });
    await scenario('best',async p=>{
      const slow=await solve(p,1,await walk(p,1,initial,['up','down']));
      const score=async expected=>assert.equal((await p.locator('#best').innerText()).trim(),String(expected));
      await score(slow.moves);
      await click(p,'next'); await match(p,2,maps[1].start);
      await score('—'); await click(p,'restart'); await match(p,2,maps[1].start);
      for (const n of [2,3]) {await solve(p,n,maps[n-1].start);await click(p,n===3?'replay':'next');}
      await p.reload(); await match(p,1,initial); await score(slow.moves);
      const fast=await solve(p,1,initial); assert.ok(fast.moves<slow.moves); await score(fast.moves);
      await p.reload(); await match(p,1,fast,'won'); await score(fast.moves);
    });
    await scenario('recovery',async p=>{
      await walk(p,1,initial,['right']);
      await p.addInitScript(()=>localStorage.setItem('night-shift.save','{broken'));
      await p.reload();await match(p,1,initial);await walk(p,1,initial,['right']);
    });
    await scenario('mobile',async p=>{
      await match(p,1,initial);
      assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      await p.screenshot({path:path.join(output,'mobile.png'),fullPage:true});
      let s=initial;
      for(const d of ['up','down','left','right']){
        const b=p.locator('[data-dir="'+d+'"]'), rect=await b.boundingBox();
        assert.ok(rect && rect.width>=44 && rect.height>=44,d+' touch target');
        await b.tap();s=move(maps[0],s,d);await match(p,1,s);
      }
    },true);
    checks.push({id:'offline',status:faults.size?'failed':'passed',evidence:[...faults].join('; ')||'No external requests, HTTP errors or uncaught script errors'});
    fs.writeFileSync(path.join(output,'browser.json'),JSON.stringify(checks));
  } finally {await browser.close();}
})().catch(err=>{console.error(err);process.exitCode=1});
