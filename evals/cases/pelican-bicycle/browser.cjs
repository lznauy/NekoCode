// Independent rendering only; no semantic or aesthetic score is inferred.
const fs = require('fs');
const {chromium} = require('playwright-core');
(async () => {
  const [file, out] = process.argv.slice(2);
  const svg = fs.readFileSync(file, 'utf8');
  const browser = await chromium.launch({executablePath: process.env.CHROME_BIN, headless: true, args: ['--no-sandbox']});
  try {
    const page = await browser.newPage();
    await page.route('**/*', route => route.abort());
    const evidence = [];
    for (const [name, width, height] of [['desktop', 1024, 768], ['mobile', 390, 600]]) {
      await page.setViewportSize({width, height});
      await page.setContent('<html><body style="margin:0;background:#eee;display:grid;place-items:center;height:100vh"><img id="art" style="width:100%;height:100%;object-fit:contain" /></body></html>');
      const stats = await page.evaluate(async source => {
        const img = document.querySelector('#art');
        img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(source);
        await img.decode();
        const canvas = document.createElement('canvas');
        canvas.width = canvas.height = 128;
        const ctx = canvas.getContext('2d');
        ctx.drawImage(img, 0, 0, 128, 128);
        const pixels = ctx.getImageData(0, 0, 128, 128).data;
        const colors = new Set(); let visible = 0;
        for (let i = 0; i < pixels.length; i += 4) {
          if (pixels[i + 3] > 16) { visible++; colors.add([pixels[i]>>4, pixels[i+1]>>4, pixels[i+2]>>4].join(',')); }
        }
        return {width: img.naturalWidth, height: img.naturalHeight, visible, colors: colors.size};
      }, svg);
      await page.screenshot({path: `${out}/${name}.png`});
      evidence.push({viewport: name, ...stats});
    }
    const passed = evidence.every(s => s.width > 0 && s.height > 0 && s.visible > 100 && s.colors > 2);
    fs.writeFileSync(out + '/render.json', JSON.stringify({id: 'rendered', status: passed ? 'passed' : 'failed', evidence: JSON.stringify(evidence) + '; nonempty rendering only, not pelican recognition'}));
  } finally { await browser.close(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
