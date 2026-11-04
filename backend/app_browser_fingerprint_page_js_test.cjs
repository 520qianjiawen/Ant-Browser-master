// Run with: node --test backend/app_browser_fingerprint_page_js_test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

function page() {
  const source = fs.readFileSync(path.join(__dirname, 'app_browser_fingerprint_page.go'), 'utf8');
  const script = source.slice(source.indexOf('<script>') + '<script>'.length, source.indexOf('</script>'))
    .replace('__FINGERPRINT_CHECK_CONTEXT__', '{}')
    .split("document.getElementById('resetBaselineBtn').onclick")[0];
  const storage = new Map();
  const sandbox = {
    URL,
    localStorage: {
      getItem: key => storage.get(key) || null,
      setItem: (key, value) => storage.set(key, value),
      removeItem: key => storage.delete(key)
    }
  };
  vm.createContext(sandbox);
  vm.runInContext(script, sandbox);
  return sandbox;
}
function report() {
  return {
    identity: { userAgent: 'Chrome/144.0.0.0', platform: 'Win32', webdriver: false },
    locale: { language: 'en-US', languages: ['en-US', 'en'], timezone: 'UTC' },
    hardware: { hardwareConcurrency: 8, deviceMemory: 8, maxTouchPoints: 0, doNotTrack: null },
    screen: { outerWidth: 1000, outerHeight: 800, width: 1920, height: 1080, colorDepth: 24, devicePixelRatio: 1 },
    network: { localCandidateCount: 1, webrtcCandidates: ['candidate:0 1 udp 1 test.local 1234 typ host generation 0'] },
    advanced: { canvasHash: 'canvas-actual', audioHash: 'audio-actual', clientRectsHash: 'rects-actual', fontHash: 'fonts-actual', detectedFonts: ['Arial'], webglVendor: 'Actual GPU', webglRenderer: 'Actual Renderer', webglHash: 'gpu-actual', plugins: [], mimeTypes: [] }
  };
}
const byName = rows => Object.fromEntries(rows.map(row => [row.name, row]));

test('unsupported metadata overrides config results but retains observations and genuine generic mismatches', () => {
  const js = page();
  const context = {
    engine: 'sunbrowser', profileId: 'sun',
    unsupportedFields: ['seed', 'disableSpoofing', 'hardwareConcurrency', 'deviceMemory', 'touchPoints', 'doNotTrack', 'colorDepth', 'brand', 'brandVersion', 'platform', 'platformVersion', 'canvasNoise', 'audioNoise', 'clientRectsNoise', 'mediaDevices', 'fontList', 'webglVendor', 'webglRenderer'],
    expected: { seed: '123', hardwareConcurrency: '8', brand: 'Chrome', platform: 'windows', language: 'fr-FR', acceptLanguage: 'fr-FR', timezone: 'Asia/Tokyo', windowSize: '1200,900', canvasNoise: 'true', clientRectsNoise: 'true' }
  };
  const rows = byName(js.buildFingerprintRows(report(), context));
  for (const name of ['Fingerprint Seed', 'Native Exposure Disabled', 'CPU Cores', 'Device Memory', 'Touch Points', 'Do Not Track', 'Color Depth', 'Brand', 'Brand Version', 'Platform', 'Platform Version', 'Canvas Noise', 'Audio Noise', 'ClientRects Noise', 'Media Device Count', 'Detected Fonts', 'WebGL Vendor', 'WebGL Renderer']) {
    assert.equal(rows[name].status, 'unsupported', name);
    assert.equal(rows[name].source, 'Engine Unsupported', name);
    assert.match(rows[name].reason, /SunBrowser.*not mapped/);
    assert.notEqual(js.statusClass(rows[name].status, rows[name].source), 'ok');
  }
  assert.equal(rows['CPU Cores'].actual, 8);
  assert.equal(rows.Brand.actual, 'Chrome/144.0.0.0');
  assert.equal(rows.Platform.actual, 'Win32');
  assert.equal(rows['Canvas Hash'].actual, 'canvas-actual');
  for (const name of ['Language', 'Languages', 'Timezone', 'Window Size']) assert.equal(rows[name].status, 'mismatch', name);
  assert.equal(rows.Webdriver.status, 'match');
  js.currentUILang = 'zh';
  assert.equal(js.uiText('Engine Unsupported'), '内核未支持');
  assert.match(js.uiText(rows.Brand.reason), /专有指纹配置尚未映射/);
});

test('metadata, not engine or UA detection, controls unsupported classification', () => {
  const js = page();
  const context = { engine: 'sunbrowser', expected: { hardwareConcurrency: '4', brand: 'Chrome' } };
  const rows = byName(js.buildFingerprintRows(report(), context));
  assert.equal(rows['CPU Cores'].status, 'mismatch');
  assert.equal(rows.Brand.status, 'match');
  context.engine = 'other';
  context.unsupportedFields = ['hardwareConcurrency'];
  assert.equal(byName(js.buildFingerprintRows(report(), context))['CPU Cores'].status, 'unsupported');
});

test('WebRTC never infers UDP policy compliance or public IP leaks solely from host counts', () => {
  const js = page();
  for (const policy of ['', 'default', 'default_public_and_private_interfaces', 'default_public_interface_only', 'future_policy']) {
    const row = byName(js.buildFingerprintRows(report(), { expected: { webrtcPolicy: policy } }))['WebRTC Host'];
    assert.equal(row.status, 'unknown', policy);
    assert.equal(row.expected, '');
    assert.equal(row.actual, '1 host candidates');
  }
  const context = { expected: { webrtcPolicy: 'disable_non_proxied_udp' } };
  const row = byName(js.buildFingerprintRows(report(), context))['WebRTC Host'];
  assert.equal(row.status, 'unknown');
  assert.equal(row.expected, 'Non-proxied UDP disabled (requested)');
  assert.equal(row.actual, '1 host candidates');
  assert.match(row.reason, /mDNS \.local.*not evidence of a public IP leak/);
  const noHosts = report();
  noHosts.network.localCandidateCount = 0;
  assert.equal(byName(js.buildFingerprintRows(noHosts, context))['WebRTC Host'].status, 'unknown');
});

test('rendered JSON report includes engine and a copied unsupported field list', () => {
  const js = page();
  js.latestContext = { engine: 'sunbrowser', unsupportedFields: ['seed'], expected: {} };
  js.updateUILanguage = js.renderMeta = js.renderChangeComparison = () => {};
  js.renderProxyCheck = js.renderFingerprintTable = () => '';
  js.document = { getElementById: () => ({ innerHTML: '' }) };
  const actual = report();
  js.render(actual);
  const exported = JSON.parse(JSON.stringify(actual));
  assert.equal(exported.engine, 'sunbrowser');
  assert.deepEqual(exported.unsupportedFields, ['seed']);
  js.latestContext.unsupportedFields.push('brand');
  assert.deepEqual(exported.unsupportedFields, ['seed']);
  assert.equal(actual.unsupportedFields.length, 1);
  assert.equal(exported.egressComparison.status, 'unavailable');
});

test('reports launch mode without presenting it as verified routing', () => {
  const js = page();
  js.latestContext = { proxy: { configured: true, launchMode: 'direct' } };
  assert.match(js.renderProxyCheck(report()), /直连参数（路由未验证）/);
  js.updateUILanguage = js.renderMeta = js.renderChangeComparison = () => {};
  js.renderProxyCheck = js.renderFingerprintTable = () => '';
  js.document = { getElementById: () => ({ innerHTML: '' }) };
  const actual = report();
  js.render(actual);
  assert.equal(actual.launchProxyMode, 'direct');
});

test('native canvas request is reported as requested, not proven', () => {
  const js = page();
  const context = { engine: 'sunbrowser', canvasNative: true, unsupportedFields: ['hardwareConcurrency'], expected: {} };
  const row = byName(js.buildFingerprintRows(report(), context))['Canvas Noise'];
  assert.equal(row.status, 'unreadable');
  assert.equal(row.expected, 'Native CanvasMark requested');
  assert.match(row.reason, /getImageData was unchanged/);
});

const autoContext = (ip, extra = {}) => ({ autoEnvironment: { ip, ipFamily: ip.includes(':') ? 'ipv6' : 'ipv4', ipSelection: 'ipv4-first', ...extra } });

test('egress comparison matches the same IPv4 without mutating actual IP or sources', () => {
  const js = page();
  const info = { ip: '203.0.113.8', source: 'browser-ip', sources: [{ source: 'original' }] };
  const original = JSON.stringify(info);
  const result = js.compareAutoEnvironmentEgress(autoContext(info.ip), info);
  assert.equal(result.status, 'match');
  assert.equal(result.expectedIP, info.ip);
  assert.equal(result.actualIP, info.ip);
  assert.equal(result.expectedFamily, 'ipv4');
  assert.equal(result.actualFamily, 'ipv4');
  assert.equal(JSON.stringify(info), original);
});

test('IPv4 versus IPv6 is a cross-family observation, not mismatch or leak proof', () => {
  const js = page();
  for (const [expected, actual] of [['203.0.113.8', '2001:db8::8'], ['2001:db8::8', '203.0.113.8']]) {
    const result = js.compareAutoEnvironmentEgress(autoContext(expected), { ip: actual });
    assert.equal(result.status, 'cross-family');
    assert.match(result.reason, /not a mismatch or proof of a leak/);
  }
});

test('browser IPv4 prefers observedIPv4 from dual-stack geo records', () => {
  const js = page();
  const context = autoContext('2001:db8::8', { observedIPv4: '203.0.113.8', ipSelection: 'dual-stack-fallback' });
  const original = JSON.stringify(context);
  const result = js.compareAutoEnvironmentEgress(context, { ip: '203.0.113.8' });
  assert.equal(result.status, 'match');
  assert.equal(result.expectedIP, '203.0.113.8');
  assert.equal(result.expectedFamily, 'ipv4');
  assert.equal(JSON.stringify(context), original);
  assert.equal(js.compareAutoEnvironmentEgress(context, { ip: '2001:db8::8' }).status, 'match');
});

test('unequal same-family IPs require investigation, not an automatic leak verdict', () => {
  const js = page();
  for (const [expected, actual] of [['203.0.113.8', '203.0.113.9'], ['2001:db8::8', '2001:db8::9']]) {
    const result = js.compareAutoEnvironmentEgress(autoContext(expected), { ip: actual });
    assert.equal(result.status, 'same-family-different');
    assert.match(result.reason, /rotating exits, proxy routing or cache; not automatically a leak/);
  }
});

test('missing metadata and invalid IPs are unavailable', () => {
  const js = page();
  assert.equal(js.compareAutoEnvironmentEgress(null, { ip: '203.0.113.8' }).status, 'unavailable');
  assert.equal(js.compareAutoEnvironmentEgress(autoContext('203.0.113.8'), null).status, 'unavailable');
  for (const invalid of ['', 'example.com', '999.1.2.3', '01.2.3.4', '127.1', '::gg', '2001:db8:::8', '[::1]:80', '::ffff:999.1.2.3', '203.0.113.8/path']) {
    assert.equal(js.compareAutoEnvironmentEgress(autoContext(invalid), { ip: '203.0.113.8' }).status, 'unavailable', invalid);
    assert.equal(js.compareAutoEnvironmentEgress(autoContext('203.0.113.8'), { ip: invalid }).status, 'unavailable', invalid);
  }
  assert.equal(js.compareAutoEnvironmentEgress(autoContext('2001:db8::8', { observedIPv4: 'invalid' }), { ip: '203.0.113.8' }).status, 'unavailable');
});

test('IPv6 compression, brackets, case and IPv4-mapped spellings normalize for comparison only', () => {
  const js = page();
  const expanded = '2001:0DB8:0000:0000:0000:0000:0000:0008';
  const result = js.compareAutoEnvironmentEgress(autoContext(expanded), { ip: '[2001:db8::8]' });
  assert.equal(result.status, 'match');
  assert.equal(result.expectedFamily, 'ipv6');
  assert.equal(result.expectedIP, expanded);
  assert.equal(result.actualIP, '[2001:db8::8]');
  for (const mapped of ['::ffff:203.0.113.8', '[::FFFF:cb00:7108]', '0:0:0:0:0:ffff:cb00:7108']) {
    const comparison = js.compareAutoEnvironmentEgress(autoContext('203.0.113.8'), { ip: mapped });
    assert.equal(comparison.status, 'match', mapped);
    assert.equal(comparison.actualFamily, 'ipv4');
    assert.equal(comparison.actualIP, mapped);
  }
});

test('proxy section translates diagnostic and render exports it without replacing network observations', () => {
  const js = page();
  js.latestContext = autoContext('2001:db8::8', { ipSelection: 'dual-stack-fallback' });
  const actual = report();
  actual.network.proxyInfo = { ok: true, ip: '203.0.113.8', source: 'original-source' };
  const original = JSON.stringify(actual.network.proxyInfo);
  js.updateUILanguage = js.renderMeta = js.renderChangeComparison = () => {};
  js.renderFingerprintTable = () => '';
  js.document = { getElementById: () => ({ innerHTML: '' }) };
  js.render(actual);
  assert.equal(actual.egressComparison.status, 'cross-family');
  assert.equal(JSON.stringify(actual.network.proxyInfo), original);
  const html = js.renderProxyCheck(actual);
  assert.match(html, /自动环境出口对比/);
  assert.match(html, /跨地址族观测/);
  assert.match(html, /不证明存在泄漏/);
  assert.match(html, /203\.0\.113\.8/);
});
