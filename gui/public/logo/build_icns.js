#!/usr/bin/env node
// Build appicon.icns from icon_<size>.png files.
// ICNS = 'icns' magic + total length, then per-icon blocks:
//   OSType (4) + block length (4, inclusive) + image data.
const fs = require('fs');
const path = require('path');

// Map: ICNS OSType -> PNG side length.
const entries = [
  { ostype: 'ic11', size: 16 },
  { ostype: 'ic11', size: 16 },   // placeholder; ic11 is 16, 1x
  { ostype: 'ic07', size: 128 },
  { ostype: 'ic08', size: 256 },
  { ostype: 'ic09', size: 512 },
  { ostype: 'ic10', size: 1024 },
  { ostype: 'ic13', size: 256 },  // 128@2x
  { ostype: 'ic14', size: 512 },  // 256@2x
];

// Use the available PNGs: 16/128/256/512/1024
// We'll skip 32/64 (no dedicated OSType) and 2x variants (no source).
const use = [
  { ostype: 'ic11', src: 'icon_16.png' },
  { ostype: 'ic07', src: 'icon_128.png' },
  { ostype: 'ic08', src: 'icon_256.png' },
  { ostype: 'ic09', src: 'icon_512.png' },
  { ostype: 'ic10', src: 'icon_1024.png' },
];

const blocks = [];
for (const e of use) {
  const data = fs.readFileSync(path.join(__dirname, e.src));
  const header = Buffer.alloc(8);
  header.write(e.ostype, 0, 4, 'ascii');
  header.writeUInt32BE(8 + data.length, 4);
  blocks.push(Buffer.concat([header, data]));
}

const body = Buffer.concat(blocks);
const head = Buffer.alloc(8);
head.write('icns', 0, 4, 'ascii');
head.writeUInt32BE(8 + body.length, 4);

const out = Buffer.concat([head, body]);
fs.writeFileSync(path.join(__dirname, 'appicon.icns'), out);
console.log(`wrote appicon.icns (${out.length} bytes, ${use.length} icons)`);
