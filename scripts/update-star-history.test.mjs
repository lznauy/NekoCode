import assert from 'node:assert/strict';
import test from 'node:test';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  buildDailySeries,
  fetchAllStargazers,
  parseLinkHeader,
  renderStarHistorySvg,
} from './update-star-history.mjs';

test('parseLinkHeader extracts pagination relationships', () => {
  assert.deepEqual(
    parseLinkHeader(
      '<https://api.github.com/example?page=2>; rel="next", <https://api.github.com/example?page=4>; rel="last"',
    ),
    {
      next: 'https://api.github.com/example?page=2',
      last: 'https://api.github.com/example?page=4',
    },
  );
});

test('fetchAllStargazers follows pages and requests timestamps', async () => {
  const requests = [];
  const fakeFetch = async (url, options) => {
    requests.push({ url: String(url), headers: options.headers });
    const page = new URL(url).searchParams.get('page');
    const body =
      page === '1'
        ? [{ starred_at: '2026-07-19T01:00:00Z' }]
        : [{ starred_at: '2026-07-20T01:00:00Z' }];
    return new Response(JSON.stringify(body), {
      headers:
        page === '1'
          ? {
              link: '<https://api.github.com/repos/example/repo/stargazers?per_page=100&page=2>; rel="next", <https://api.github.com/repos/example/repo/stargazers?per_page=100&page=2>; rel="last"',
            }
          : {},
      status: 200,
    });
  };

  assert.deepEqual(
    await fetchAllStargazers({
      repository: 'example/repo',
      token: 'fake-token',
      fetchImpl: fakeFetch,
    }),
    ['2026-07-19T01:00:00Z', '2026-07-20T01:00:00Z'],
  );
  assert.equal(requests.length, 2);
  assert.equal(requests[0].headers.Accept, 'application/vnd.github.star+json');
  assert.equal(requests[0].headers.Authorization, 'Bearer fake-token');
});

test('fetchAllStargazers does not retry permission failures', async () => {
  let requests = 0;
  const fakeFetch = async () => {
    requests += 1;
    return new Response('{"message":"Forbidden"}', { status: 403 });
  };

  await assert.rejects(
    fetchAllStargazers({
      repository: 'example/repo',
      token: 'fake-token',
      fetchImpl: fakeFetch,
    }),
    /GitHub API returned 403/,
  );
  assert.equal(requests, 1);
});

test('buildDailySeries aggregates days and extends to the current date', () => {
  assert.deepEqual(
    buildDailySeries(
      [
        '2026-07-19T01:00:00Z',
        '2026-07-19T18:00:00Z',
        '2026-07-20T01:00:00Z',
      ],
      new Date('2026-07-21T12:00:00Z'),
    ),
    [
      { date: '2026-07-19', count: 2 },
      { date: '2026-07-20', count: 3 },
      { date: '2026-07-21', count: 3 },
    ],
  );
});

test('renderStarHistorySvg produces accessible light and dark charts', () => {
  const series = [
    { date: '2026-07-19', count: 1 },
    { date: '2026-07-21', count: 3 },
  ];
  const light = renderStarHistorySvg(series, { repository: 'example/repo', theme: 'light' });
  const dark = renderStarHistorySvg(series, { repository: 'example/repo', theme: 'dark' });

  assert.match(light, /<title id="title">example\/repo Star History<\/title>/);
  assert.match(light, /3 stars/);
  assert.match(light, /#ffffff/);
  assert.match(dark, /#0d1117/);
  assert.doesNotMatch(light, /NaN|undefined/);
});


test('publishing charts disables Vercel on the assets branch and remains idempotent', (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'star-history-publish-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const remote = join(directory, 'remote.git');
  const checkout = join(directory, 'checkout');
  const output = join(directory, 'output');
  mkdirSync(checkout);
  mkdirSync(output);
  const git = (...args) => execFileSync('git', args, { cwd: checkout, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
  git('init', '--bare', remote);
  git('init', '-b', 'master');
  git('config', 'user.name', 'Test');
  git('config', 'user.email', 'test@example.invalid');
  writeFileSync(join(checkout, 'README.md'), 'Main branch');
  git('add', '.');
  git('commit', '-m', 'initial');
  git('remote', 'add', 'origin', remote);
  git('push', 'origin', 'master');
  git('switch', '--orphan', 'star-history');
  mkdirSync(join(checkout, 'assets/star-history'), { recursive: true });
  for (const theme of ['light', 'dark']) {
    const name = `star-history-${theme}.svg`;
    writeFileSync(join(checkout, 'assets/star-history', name), '<svg>old</svg>');
    writeFileSync(join(output, name), '<svg>new</svg>');
  }
  git('add', '.');
  git('commit', '-m', 'old charts');
  git('push', 'origin', 'star-history');
  git('switch', 'master');
  const workflow = readFileSync(new URL('../.github/workflows/update-star-history.yml', import.meta.url), 'utf8');
  const script = workflow.split('        run: |\n')[1].replace(/^          /gm, '');
  const publish = () => execFileSync('bash', ['-c', script], {
    cwd: checkout,
    env: { ...process.env, OUTPUT_DIR: output, TARGET_BRANCH: 'star-history' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  publish();
  for (const path of ['vercel.json', 'official/vercel.json']) {
    const config = JSON.parse(git('show', `origin/star-history:${path}`));
    assert.equal(config.git.deploymentEnabled, false);
  }
  assert.equal(git('show', 'origin/star-history:assets/star-history/star-history-light.svg'), '<svg>new</svg>');
  assert.equal(git('ls-tree', '-r', '--name-only', 'master').trim(), 'README.md');
  const published = git('rev-parse', 'origin/star-history');
  publish();
  assert.equal(git('rev-parse', 'origin/star-history'), published);
});
