import { closeSync, mkdirSync, openSync } from 'node:fs';

mkdirSync('dist', { recursive: true });
closeSync(openSync('dist/.gitkeep', 'a'));
