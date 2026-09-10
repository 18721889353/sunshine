/**
 * Post-build script: Patch index.html for Go embed.FS serving.
 * 
 * 1. Add appConfig.js script tag
 * 2. Add /static/ prefix to all asset URLs
 */
import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const indexPath = resolve(__dirname, '../../cmd/sunshine/server/static/index.html');

let html = readFileSync(indexPath, 'utf-8');

// Add appConfig.js script tag before </head>
html = html.replace(
  '</head>',
  '  <script type="text/javascript" src="/static/appConfig.js" async></script>\n</head>'
);

// Add /static/ prefix to asset references (CSS and JS files)
// Match href="/assets/... and src="/assets/...
html = html.replace(/(href|src)="\/assets\//g, '$1="/static/assets/');

// Rewrite favicon to the simple static path (Vite may not copy public/favicon.png)
html = html.replace(
  /href="[^"]*favicon[^"]*"/,
  'href="/static/favicon.png"'
);

writeFileSync(indexPath, html, 'utf-8');
console.log('Patched index.html successfully');
