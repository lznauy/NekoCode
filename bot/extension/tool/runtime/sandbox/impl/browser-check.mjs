// Usage: node --input-type=module -e "${NEKOCODE_BROWSER_CHECK:?Linux sandbox required}" -- URL 'predicate' 'do:action()' 'predicate'
import { spawn } from 'node:child_process'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { mkdtemp, readFile, realpath, rename, rm, stat, writeFile } from 'node:fs/promises'
import { networkInterfaces, tmpdir } from 'node:os'
import { basename, dirname, extname, join, relative } from 'node:path'
import { pathToFileURL } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'

const [address, ...args] = process.argv.slice(1)
const screenshot = address === '--screenshot'
const staticPage = address === '--static'
const steps = screenshot ? [] : staticPage ? args.slice(1) : args
const hasAssertion = steps.some(step => step.trim() && !step.startsWith('do:') && !step.startsWith('observe:'))
const hasObservation = steps.some(step => step.startsWith('observe:') && step.slice(8).trim())
let url, output, viewport, clip
if (screenshot) {
  const [input, destination, size = '1024x768', region] = args
  if (!input || !destination || args.length > 4 || !['.html', '.htm', '.svg'].includes(extname(input).toLowerCase()) ||
      extname(destination).toLowerCase() !== '.png' || !/^\d{1,4}x\d{1,4}$/.test(size)) {
    throw Error('Usage: --screenshot INPUT.svg|html OUTPUT.png [WIDTHxHEIGHT] [X,Y,W,H]')
  }
  const [width, height] = size.split('x').map(Number)
  if (!width || !height || width * height > 16000000) throw Error('Invalid viewport (maximum 16 MP)')
  viewport = { width, height, deviceScaleFactor: 1, mobile: false }
  if (region !== undefined) {
    if (!/^\d{1,4}(,\d{1,4}){3}$/.test(region)) throw Error('Expected clip X,Y,W,H in viewport pixels')
    const [x, y, w, h] = region.split(',').map(Number)
    if (!w || !h || x + w > width || y + h > height) throw Error('Clip outside viewport')
    clip = { x, y, width: w, height: h, scale: 1 }
  }
  url = pathToFileURL(await realpath(input))
  output = destination
} else if (!staticPage) {
  url = new URL(address)
  if (url.protocol !== 'http:' || !['127.0.0.1', '[::1]'].includes(url.hostname) || (!hasAssertion && !hasObservation)) {
    throw Error('Expected an HTTP loopback URL and at least one page assertion or observation')
  }
}

const profile = await mkdtemp(join(tmpdir(), 'nekocode-chrome-'))
let browser, server
let closed
try {
  if (staticPage) {
    if (!args[0] || (!hasAssertion && !hasObservation)) throw Error('Usage: --static ENTRY.html predicate [do:action predicate ...]')
    const entry = await realpath(args[0])
    if (!/\.html?$/i.test(entry) || !(await stat(entry)).isFile()) throw Error('Expected a static HTML entry file')
    const root = dirname(entry)
    const types = { '.html':'text/html', '.htm':'text/html', '.js':'text/javascript', '.mjs':'text/javascript',
      '.css':'text/css', '.json':'application/json', '.svg':'image/svg+xml', '.wasm':'application/wasm' }
    server = createServer(async (req,res) => {
      try {
        const name = decodeURIComponent(new URL(req.url,'http://localhost').pathname)
        const file = await realpath(join(root, name === '/' ? basename(entry) : name))
        const parts = relative(root,file).split('/')
        if (req.method !== 'GET' || parts.some(part=>part.startsWith('.')) || !(await stat(file)).isFile()) throw Error()
        const bytes = await readFile(file)
        res.writeHead(200, {'Content-Type': types[extname(file).toLowerCase()] || 'application/octet-stream'})
        res.end(bytes)
      } catch { res.writeHead(404); res.end() }
    })
    server.listen(0, '127.0.0.1')
    await once(server, 'listening')
    url = new URL('/'+encodeURIComponent(basename(entry)), 'http://127.0.0.1:'+server.address().port)
  }
  // Chrome refuses UID 0 even inside the shell's single-user namespace.
  const mappedRoot = process.platform === 'linux' && process.getuid() === 0 &&
    Object.values(networkInterfaces()).flat().every(address => address.internal) &&
    /^0\s+[1-9]\d*\s+1$/.test((await readFile('/proc/self/uid_map', 'utf8')).trim())
  browser = spawn(process.env.CHROME_BIN || 'google-chrome', [
    '--headless=new', '--disable-dev-shm-usage', '--disable-extensions',
    ...(mappedRoot ? ['--no-sandbox'] : []), '--remote-debugging-pipe', '--user-data-dir=' + profile, 'about:blank'
  ], { env: { ...process.env, XDG_CONFIG_HOME: profile }, stdio: ['ignore', 'ignore', 'pipe', 'pipe', 'pipe'] })
  let diagnostic = ''
  browser.stderr.setEncoding('utf8').on('data', chunk => { diagnostic = (diagnostic + chunk).slice(-700) })
  closed = new Promise(resolve => browser.once('close', resolve))
  await once(browser, 'spawn')
  let buffer = ''
  let seq = 0
  const pending = new Map()
  // Document responses may arrive before Page.navigate returns its loader ID.
  const documents = new Map()
  browser.stdio[4].setEncoding('utf8').on('data', chunk => {
    buffer += chunk
    let end
    while ((end = buffer.indexOf('\0')) >= 0) {
      const message = JSON.parse(buffer.slice(0, end))
      buffer = buffer.slice(end + 1)
      if (message.method === 'Network.responseReceived' && message.params.type === 'Document') {
        documents.set(message.params.loaderId, message.params.response.status)
      }
      pending.get(message.id)?.(message)
    }
  })
  browser.stdio[3].on('error', () => {}) // A closed browser also closes its debugging pipe.
  async function call(method, params = {}, sessionId) {
    const id = ++seq
    let removeExitListener
    let timer
    try {
      const message = await new Promise((resolve, reject) => {
        const onExit = () => reject(Error('Chrome exited: ' + diagnostic.trim()))
        browser.once('exit', onExit)
        removeExitListener = () => browser.off('exit', onExit)
        if (browser.exitCode !== null || browser.signalCode !== null) return onExit()
        pending.set(id, resolve)
        timer = setTimeout(() => reject(Error(method + ' timed out')), 5000)
        browser.stdio[3].write(JSON.stringify({ id, method, params, sessionId }) + '\0', err => err && reject(err))
      })
      if (message.error) throw Error(message.error.message)
      return message.result
    } finally {
      removeExitListener?.()
      clearTimeout(timer)
      pending.delete(id)
    }
  }
  const { targetId } = await call('Target.createTarget', { url: 'about:blank' })
  const { sessionId } = await call('Target.attachToTarget', { targetId, flatten: true })
  if (screenshot) await call('Emulation.setDeviceMetricsOverride', viewport, sessionId)
  if (!screenshot) await call('Network.enable', {}, sessionId)
  const deadline = Date.now() + 8000
  for (;;) {
    const navigation = await call('Page.navigate', { url: url.href }, sessionId)
    if (screenshot) {
      if (navigation.errorText) throw Error(navigation.errorText)
      break
    }
    while (!navigation.errorText && !documents.has(navigation.loaderId) && Date.now() < deadline) await delay(25)
    const status = documents.get(navigation.loaderId)
    if (!navigation.errorText && status >= 200 && status < 300) break
    const problem = navigation.errorText || (status ? 'HTTP ' + status : 'missing document response')
    if ((status >= 400 && status < 500) || Date.now() >= deadline) throw Error('Page failed to load: ' + problem)
    await delay(100)
  }
  const loaded = Date.now() + 5000
  for (;;) {
    const ready = await call('Runtime.evaluate', { expression: "location.href !== 'about:blank' && document.readyState !== 'loading'" }, sessionId)
    if (ready.result.value === true) break
    if (Date.now() >= loaded) throw Error('Page did not load')
    await delay(100)
  }
  for (const [index, step] of steps.entries()) {
    const action = step.startsWith('do:')
    const observe = step.startsWith('observe:')
    const expression = observe ? step.slice(8) : action ? step.slice(3) : step
    const until = Date.now() + 5000
    for (;;) {
      const result = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, sessionId)
      const failure = result.exceptionDetails
      const detail = failure?.exception?.description || failure?.text
      if (observe) {
        if (failure || !Object.hasOwn(result.result,'value')) throw Error('Observation ' + (index+1) + ': ' + (detail || 'expected a JSON value'))
        const value = JSON.stringify(result.result.value)
        if (value.length > 4096) throw Error('Observation ' + (index+1) + ': select a smaller JSON value (max 4096 characters)')
        console.log('Observation ' + (index+1) + ': ' + value)
        break
      }
      if (action || failure?.exception?.className === 'SyntaxError') {
        if (failure) throw Error('Step ' + (index + 1) + ': ' + detail)
        break
      }
      if (!failure && result.result.value === true) break
      if (Date.now() >= until) throw Error('Assertion ' + (index + 1) + ' timed out: ' + expression + (detail ? ' — ' + detail : ''))
      await delay(100)
    }
  }
  if (screenshot) {
    const ready = await call('Runtime.evaluate', {
      expression: 'Promise.all([document.fonts.ready, ...Array.from(document.images || [], img => img.decode())]).then(() => !document.querySelector("parsererror"))',
      awaitPromise: true, returnByValue: true
    }, sessionId)
    if (ready.result?.value !== true) throw Error('Document or images failed to load')
    const { data } = await call('Page.captureScreenshot', { format: 'png', clip }, sessionId)
    // Publish only a complete PNG, including when refreshing a prior preview.
    const staging = await mkdtemp(join(dirname(output), '.nekocode-preview-'))
    try {
      const file = join(staging, 'preview.png')
      await writeFile(file, Buffer.from(data, 'base64'))
      await rename(file, output)
    } finally {
      await rm(staging, { recursive: true, force: true })
    }
    console.log('Screenshot saved: ' + output)
  } else if (hasAssertion) {
    console.log('Browser check passed (' + steps.length + ' steps)')
  }
} catch (err) {
  console.error('Browser check failed: ' + err.message)
  process.exitCode = 1
} finally {
  if (browser?.pid) {
    browser.kill('SIGTERM')
    const timer = setTimeout(() => browser.kill('SIGKILL'), 1000)
    await closed
    clearTimeout(timer)
  }
  server?.close()
  server?.closeAllConnections()
  await rm(profile, { recursive: true, force: true, maxRetries: 4 })
}
