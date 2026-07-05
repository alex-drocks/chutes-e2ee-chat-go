import { readFileSync, writeFileSync } from 'node:fs';

const version = process.argv[2] ?? process.env.RELEASE_VERSION;

if (!/^\d+\.\d+\.\d+$/.test(version ?? '')) {
  throw new Error(`Version '${version}' must use MAJOR.MINOR.PATCH format.`);
}

function updateJson(path, update) {
  const data = JSON.parse(readFileSync(path, 'utf8'));
  update(data);
  writeFileSync(path, `${JSON.stringify(data, null, 2)}\n`);
}

for (const path of ['package.json', 'frontend/package.json']) {
  updateJson(path, (data) => {
    data.version = version;
  });
}

updateJson('frontend/package-lock.json', (data) => {
  if (data.packages?.['']) {
    data.packages[''].version = version;
  }
});

updateJson('wails.json', (data) => {
  data.info ??= {};
  data.info.companyName ??= 'alex-drocks';
  data.info.productName = 'Chutes E2EE Chat';
  data.info.productVersion = version;
  data.info.copyright ??= 'Copyright 2026 alex-drocks';
  data.info.comments ??= 'Wails + Go desktop client for Chutes.ai E2EE chat.';
});
