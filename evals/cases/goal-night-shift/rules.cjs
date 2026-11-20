// Independent grid model. Cell numbers are row-major within this level.
const directions = { up: [0,-1], down: [0,1], left: [-1,0], right: [1,0] };

function level(rows) {
  const width = rows[0].length, tiles = [], boxes = [];
  let player;
  for (const c of rows.join('')) {
    const cell = tiles.length;
    tiles.push(c === '#' ? 'wall' : c === '.' ? 'goal' : 'floor');
    if (c === '$') boxes.push(cell);
    if (c === '@') player = cell;
  }
  return {width, tiles, start: {player, boxes, moves: 0, pushes: 0}};
}

function move(map, state, direction) {
  const [x,y] = directions[direction], stride = x + y * map.width;
  const to = state.player + stride, boxes = [...state.boxes];
  if (map.tiles[to] === 'wall') return state;
  const i = boxes.indexOf(to);
  if (i >= 0) {
    if (map.tiles[to+stride] === 'wall' || boxes.includes(to+stride)) return state;
    boxes[i] = to+stride;
    boxes.sort((a,b) => a-b);
  }
  return {player: to, boxes, moves: state.moves+1, pushes: state.pushes + Number(i >= 0)};
}

const won = (map,state) => state.boxes.every(c => map.tiles[c] === 'goal');

function pathTo(map, initial, predicate) {
  const queue = [{state:initial, previous:-1}], seen = new Set();
  for (let i=0; i<queue.length; i++) {
    const node = queue[i], s = node.state;
    if (predicate(s)) {
      const path = [];
      for (let at=i; queue[at].previous >= 0; at=queue[at].previous) path.push(queue[at].direction);
      return path.reverse();
    }
    if (won(map,s)) continue;
    for (const d of Object.keys(directions)) {
      const next = move(map,s,d), key = next.player + ':' + next.boxes.join(',');
      if (next === s || seen.has(key)) continue;
      seen.add(key);
      queue.push({state:next,previous:i,direction:d});
    }
  }
  throw Error('Published map has no path to the requested state');
}
module.exports = {level, move, won, pathTo, directions};
