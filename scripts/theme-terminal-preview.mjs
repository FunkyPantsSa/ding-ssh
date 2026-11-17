#!/usr/bin/env node
/**
 * Generates the dark "terminal + SFTP" acceptance layouts for the five theme
 * hues. The colour values come from the real material.ts engine; the DOM uses
 * the application's own shell classes and stylesheet, so the previews exercise
 * the same tokens as the app rather than hand-picked swatches.
 *
 * Usage:
 *   node --experimental-strip-types scripts/theme-terminal-preview.mjs
 *
 * Requires Google Chrome for the headless screenshots. Output:
 *   docs/theme-dark-terminal-<theme>.png (1600×1040)
 */
import {execFileSync} from 'node:child_process'
import {mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {dirname, join, resolve} from 'node:path'
import {fileURLToPath, pathToFileURL} from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const DOCS = join(ROOT, 'docs')
const OUT_DIR = process.env.PREVIEW_OUT_DIR ? resolve(ROOT, process.env.PREVIEW_OUT_DIR) : DOCS
const CHROME =
  process.env.CHROME_BIN ??
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'

const {deriveColorRoles, planMaterial, rolesToCssVars, hexToOklch} = await import(
  pathToFileURL(join(ROOT, 'frontend/src/theme/material.ts')).href
)
const {Hct, argbFromHex} = await import(
  pathToFileURL(join(ROOT, 'frontend/node_modules/@material/material-color-utilities/index.js')).href
)

function metrics(hex) {
  const hct = Hct.fromInt(argbFromHex(hex))
  const oklch = hexToOklch(hex)
  return {
    hex,
    tone: Number(hct.tone.toFixed(1)),
    hctChroma: Number(hct.chroma.toFixed(1)),
    oklchChroma: Number(oklch.c.toFixed(4)),
    oklchHue: Number(oklch.h.toFixed(1)),
  }
}

const THEMES = [
  {id: 'blue', name: '蓝色', seed: '#87CEEB'},
  {id: 'pink', name: '粉色', seed: '#E89AB8'},
  {id: 'green', name: '绿色', seed: '#4AB07A'},
  {id: 'yellow', name: '黄色', seed: '#D4A04A'},
  {id: 'purple', name: '紫色', seed: '#A78BFA'},
]

const APPEARANCE = {atmosphere: 75, separation: 65}

function stripAtImports(css) {
  return css
    .split('\n')
    .filter((line) => !line.trimStart().startsWith('@import'))
    .join('\n')
}

function readThemeCss() {
  const themeDir = join(ROOT, 'frontend/src/theme')
  const parts = [
    join(themeDir, 'tokens.css'),
    join(themeDir, 'semantic.css'),
    join(themeDir, 'components.css'),
    join(themeDir, 'tailwind.css'),
    join(themeDir, 'terminal.css'),
    join(ROOT, 'frontend/src/style.css'),
    join(ROOT, 'scripts/theme-terminal-preview.css'),
  ]
  return parts.map((file) => stripAtImports(readFileSync(file, 'utf8'))).join('\n')
}

function tokensFor(seed) {
  const roles = deriveColorRoles({
    seed,
    tone: 'dark',
    atmosphere: APPEARANCE.atmosphere,
    separation: APPEARANCE.separation,
  })
  const plan = planMaterial({mode: 'solid', nativeBackdrop: false, platform: 'web'})
  const vars = rolesToCssVars(roles, plan)
  return {
    roles,
    css: [
      ...Object.entries(vars).map(([key, value]) => `${key}:${value};`),
      '--terminal-fg:#fafafa;',
      '--nav-w:250px;',
      '--tool-w:340px;',
      '--topbar-h:50px;',
      '--font-mono:"Geist Mono", "SF Mono", Menlo, Monaco, "Cascadia Mono", "Noto Sans SC", monospace;',
    ].join('\n'),
  }
}

function buildHtml(template, style, theme, roles) {
  const replacements = {
    __STYLE__: style,
    __THEME_NAME__: theme.name,
    __SEED__: theme.seed.toUpperCase(),
    __TERMINAL_BG__: roles.terminalBg,
    __TOOLBAR_BG__: roles.toolbarBg,
    __SFTP_BG__: roles.surfaceBg,
    __NAV_BG__: roles.sidebarPrimaryBg,
  }
  let html = template
  for (const [key, value] of Object.entries(replacements)) {
    html = html.split(key).join(value)
  }
  return html
}

function runChrome(url, output) {
  const profile = mkdtempSync(join(tmpdir(), 'ding-ssh-preview-'))
  try {
    execFileSync(
      CHROME,
      [
        '--headless=new',
        // The DSH workspace sandbox already isolates this process; Chrome's
        // own nested sandbox cannot initialise inside it on macOS.
        '--no-sandbox',
        '--disable-gpu',
        '--disable-crash-reporter',
        '--hide-scrollbars',
        '--force-device-scale-factor=1',
        '--window-size=1600,1040',
        '--virtual-time-budget=1500',
        '--run-all-compositor-stages-before-draw',
        '--allow-file-access-from-files',
        '--no-first-run',
        '--no-default-browser-check',
        `--user-data-dir=${profile}`,
        `--screenshot=${output}`,
        url,
      ],
      {stdio: 'pipe'},
    )
  } finally {
    rmSync(profile, {recursive: true, force: true})
  }
}

mkdirSync(OUT_DIR, {recursive: true})
const template = readFileSync(join(ROOT, 'scripts/theme-terminal-preview.html'), 'utf8')
const appCss = readThemeCss()
const work = mkdtempSync(join(tmpdir(), 'ding-ssh-preview-html-'))
const results = []

try {
  for (const theme of THEMES) {
    const {roles, css} = tokensFor(theme.seed)
    const style = `${appCss}\n:root{\n${css}\n}`
    const html = buildHtml(template, style, theme, roles)
    const htmlPath = join(work, `${theme.id}.html`)
    const output = join(OUT_DIR, `theme-dark-terminal-${theme.id}.png`)
    writeFileSync(htmlPath, html)
    runChrome(pathToFileURL(htmlPath).href, output)
    results.push({
      id: theme.id,
      name: theme.name,
      seed: theme.seed,
      terminal: roles.terminalBg,
      toolbar: roles.toolbarBg,
      sftp: roles.surfaceBg,
      nav: roles.sidebarPrimaryBg,
      metrics: {
        terminal: metrics(roles.terminalBg),
        toolbar: metrics(roles.toolbarBg),
        sftp: metrics(roles.surfaceBg),
        nav: metrics(roles.sidebarPrimaryBg),
      },
      output,
    })
    console.log(
      `${theme.name.padEnd(2, ' ')} ${theme.seed}  terminal ${roles.terminalBg}  toolbar ${roles.toolbarBg}  sftp ${roles.surfaceBg}  nav ${roles.sidebarPrimaryBg}`,
    )
  }
} finally {
  rmSync(work, {recursive: true, force: true})
}

writeFileSync(
  join(OUT_DIR, 'theme-dark-terminal-colors.json'),
  `${JSON.stringify(
    results.map(({id, name, seed, terminal, toolbar, sftp, nav, metrics}) => ({
      id,
      name,
      seed,
      terminal,
      toolbar,
      sftp,
      nav,
      metrics,
    })),
    null,
    2,
  )}\n`,
)

console.log(`\nWrote ${results.length} previews to ${OUT_DIR}`)
