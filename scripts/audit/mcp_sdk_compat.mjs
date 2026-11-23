// Install the official SDK outside the app's dependency tree, for example:
// npm install --prefix /tmp/ding-sdk-test @modelcontextprotocol/sdk@1.32.2
// node scripts/audit/mcp_sdk_compat.mjs --sdk-root /tmp/ding-sdk-test \
//   --debug-info /absolute/test-config/debug.json --stdio-bin /absolute/ding-ssh-mcp
import {readFile} from 'node:fs/promises';
import {resolve} from 'node:path';
import {createRequire} from 'node:module';
import {pathToFileURL} from 'node:url';
import {parseArgs} from 'node:util';

const {values} = parseArgs({options: {
  'sdk-root': {type: 'string'}, 'debug-info': {type: 'string'}, 'stdio-bin': {type: 'string'},
}});
if (!values['sdk-root'] || !values['debug-info'] || !values['stdio-bin']) {
  throw new Error('Required: --sdk-root, --debug-info, --stdio-bin');
}
const requireSDK = createRequire(resolve(values['sdk-root'], 'package.json'));
const loadSDK = name => import(pathToFileURL(requireSDK.resolve('@modelcontextprotocol/sdk/client/' + name)));
const {Client} = await loadSDK('index.js');
const {StreamableHTTPClientTransport} = await loadSDK('streamableHttp.js');
const {StdioClientTransport} = await loadSDK('stdio.js');
const discovery = resolve(values['debug-info']);
const info = JSON.parse(await readFile(discovery, 'utf8'));
const results = [];
for (const kind of ['http', 'stdio']) {
  const client = new Client({name: 'official-sdk-compat', version: '1'}, {capabilities: {}});
  const transport = kind === 'http'
    ? new StreamableHTTPClientTransport(new URL('http://127.0.0.1:' + info.port + '/mcp'), {requestInit: {headers: {Authorization: 'Bearer ' + info.token}}})
    : new StdioClientTransport({command: resolve(values['stdio-bin']), args: ['--debug-json', discovery, '--quiet'], stderr: 'pipe'});
  try {
    await client.connect(transport);
    const tools = await client.listTools();
    const templates = await client.listResourceTemplates();
    const health = await client.callTool({name: 'app.health', arguments: {}});
    if (health.isError) throw new Error('Health check failed');
    results.push({transport: kind, tools: tools.tools.length, templates: templates.resourceTemplates.length, health: true});
  } finally { await client.close(); }
}
// Do not print discovery information or credentials, including on failure.
process.stdout.write(JSON.stringify(results, null, 2) + '\n');
