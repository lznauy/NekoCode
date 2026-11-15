// Independent browser probe. Dependencies are supplied by the evaluator, never the agent.
const fs = require('fs');
const { chromium } = require('playwright-core');
(async () => {
  const [url,out] = process.argv.slice(2), checks=[], errors=[];
  const browser = await chromium.launch({executablePath:process.env.CHROME_BIN,headless:true,args:['--no-sandbox']});
  const page=await browser.newPage();
  page.setDefaultTimeout(1500);
  page.on('pageerror',e=>errors.push(String(e)));
  await page.clock.install();
  await page.clock.pauseAt(new Date(Date.now()+1000));
  const check=async(id,fn)=>{try{await fn();checks.push({id,status:'passed',evidence:'real Chrome: '+id});}catch(e){checks.push({id,status:'failed',evidence:String(e)});}};
  const assert=(b,s)=>{if(!b)throw Error(s);};
  const state=()=>page.locator('#board [data-x][data-y]').evaluateAll(xs=>xs.map(e=>({x:+e.dataset.x,y:+e.dataset.y,state:e.dataset.state})));
  const signature=s=>s.filter(c=>c.state==='snake').map(c=>`${c.x},${c.y}`).sort().join(';');
  await page.goto(url);
  await check('board',async()=>{const s=await state();assert(s.length===144,'12x12 board missing');assert(new Set(s.map(c=>`${c.x},${c.y}`)).size===144&&s.every(c=>c.x>=0&&c.x<12&&c.y>=0&&c.y<12),'invalid coordinates');assert(s.filter(c=>c.state==='snake').length===3,'initial length');assert(s.filter(c=>c.state==='food').length===1,'food count');assert((await page.locator('#status').textContent()).trim()==='ready','not ready');const a=signature(s);await page.clock.runFor(400);assert(signature(await state())===a,'ready moved');});
  await check('start_move',async()=>{await page.locator('#start').click();const a=signature(await state());await page.clock.runFor(200);assert(signature(await state())!==a,'no movement');assert((await page.locator('#status').textContent()).trim()==='running','not running');});
  await check('steer',async()=>{await page.locator('#restart').click();const before=await state();await page.keyboard.press('ArrowDown');await page.clock.runFor(200);const after=await state();assert(signature(after)!==signature(before),'steer did not move');const added=after.filter(c=>c.state==='snake'&&!before.some(b=>b.x===c.x&&b.y===c.y&&b.state==='snake'));assert(added.length===1&&before.some(b=>b.state==='snake'&&b.x===added[0].x&&b.y===added[0].y-1),'down turn incorrect');});
  async function feed(target){
    await page.locator('#restart').click();let dir=[1,0],before=await state();
    await page.clock.runFor(200);let s=await state();let head=s.find(c=>c.state==='snake'&&!before.some(b=>b.state==='snake'&&b.x===c.x&&b.y===c.y));assert(head,'cannot discover head');
    for(let step=0;step<432;step++){
      const score=+(await page.locator('#score').textContent());s=await state();
      if(score>=target){assert(s.filter(c=>c.state==='snake').length===3+score,'growth/score wrong');assert(s.filter(c=>c.state==='food').length===1,'food count');return {head,dir};}
      const food=s.find(c=>c.state==='food'),body=new Set(s.filter(c=>c.state==='snake').map(c=>`${c.x},${c.y}`));assert(food,'food missing');
      const q=[{x:head.x,y:head.y,path:[]}],seen=new Set([`${head.x},${head.y}`]);let route;
      while(q.length){const n=q.shift();if(n.x===food.x&&n.y===food.y){route=n.path;break;}for(const [dx,dy,key] of [[1,0,'ArrowRight'],[-1,0,'ArrowLeft'],[0,1,'ArrowDown'],[0,-1,'ArrowUp']]){if(n.path.length===0&&dx===-dir[0]&&dy===-dir[1])continue;const x=n.x+dx,y=n.y+dy,k=`${x},${y}`;if(x<0||x>11||y<0||y>11||body.has(k)||seen.has(k))continue;seen.add(k);q.push({x,y,path:[...n.path,[dx,dy,key]]});}}
      assert(route&&route.length,'no route to food');const [dx,dy,key]=route[0];await page.keyboard.press(key);await page.clock.runFor(200);head={x:head.x+dx,y:head.y+dy};dir=[dx,dy];
    }throw Error('food not reached');
  }
  await check('food_growth',async()=>{await feed(1);});
  await check('reverse',async()=>{await page.locator('#restart').click();const before=await state();await page.keyboard.press('ArrowLeft');await page.clock.runFor(200);const s=await state();assert((await page.locator('#status').textContent()).trim()==='running','direct reverse caused collision');const added=s.find(c=>c.state==='snake'&&!before.some(b=>b.state==='snake'&&b.x===c.x&&b.y===c.y));assert(added&&before.some(b=>b.state==='snake'&&b.x===added.x-1&&b.y===added.y),'direct reverse was accepted');});
  await check('self_collision',async()=>{
    const {head,dir}=await feed(3),keys={'1,0':'ArrowRight','-1,0':'ArrowLeft','0,1':'ArrowDown','0,-1':'ArrowUp'};
    const turns=[[dir[1],-dir[0]],[-dir[1],dir[0]]];
    const perpendicular=turns.find(([dx,dy])=>head.x+dx>=0&&head.x+dx<12&&head.y+dy>=0&&head.y+dy<12&&head.x+dx-dir[0]>=0&&head.x+dx-dir[0]<12&&head.y+dy-dir[1]>=0&&head.y+dy-dir[1]<12);
    assert(perpendicular,'cannot construct in-bounds collision route');
    for(const d of [perpendicular,[-dir[0],-dir[1]],[-perpendicular[0],-perpendicular[1]],dir]){if((await page.locator('#status').textContent()).trim()==='over')break;await page.keyboard.press(keys[d.join(',')]);await page.clock.runFor(200);}
    assert((await page.locator('#status').textContent()).trim()==='over','self collision not terminal');
  });
  await check('pause',async()=>{await page.locator('#restart').click();await page.locator('#pause').click();assert((await page.locator('#status').textContent()).trim()==='paused','not paused');const a=signature(await state());await page.clock.runFor(600);assert(signature(await state())===a,'paused moved');await page.locator('#pause').click();await page.clock.runFor(200);assert(signature(await state())!==a,'resume did not move');});
  await check('wall_collision',async()=>{await page.locator('#restart').click();await page.clock.runFor(2600);assert((await page.locator('#status').textContent()).trim()==='over','wall collision not terminal');const a=signature(await state());await page.clock.runFor(400);assert(signature(await state())===a,'game-over still moves');});
  await check('restart',async()=>{for(let i=0;i<4;i++)await page.locator('#restart').click();assert(+(await page.locator('#score').textContent())===0,'score not reset');const before=await state();assert(before.filter(c=>c.state==='snake').length===3,'length not reset');await page.clock.runFor(199);assert(signature(await state())===signature(before),'duplicate timer/early tick');await page.clock.runFor(1);const after=await state();assert(after.filter(c=>c.state==='snake'&&!before.some(b=>b.state==='snake'&&b.x===c.x&&b.y===c.y)).length===1,'restart movement not one step');});
  await check('browser_errors',async()=>assert(errors.length===0,errors.join(';')));
  await page.screenshot({path:out+'/product.png'});
  fs.writeFileSync(out+'/result.json',JSON.stringify({schema_version:1,checks}));
  await browser.close();
})().catch(e=>{console.error(e);process.exit(2)});
