<#
ding-ssh 调试模式辅助脚本（Windows PowerShell）

用法：
  .\scripts\debug.ps1 health                       # 调试服务状态
  .\scripts\debug.ps1 state                        # 应用状态快照（会话/标签/设置）
  .\scripts\debug.ps1 terminals                    # 终端诊断（rows/baseY/viewportY/几何/可见文本）
  .\scripts\debug.ps1 servers                      # 服务器列表（默认脱敏）
  .\scripts\debug.ps1 buffer  <id> [lines]         # 读终端缓冲区（含回滚）
  .\scripts\debug.ps1 input   <id> "echo hi`r"     # 向会话写入
  .\scripts\debug.ps1 scroll  <id> top|bottom|line <value>
  .\scripts\debug.ps1 resize  <id> <cols> <rows>
  .\scripts\debug.ps1 open    <serverId> | .\scripts\debug.ps1 open -Local
  .\scripts\debug.ps1 close   <id>
  .\scripts\debug.ps1 reconnect <id> | disconnect <id>
  .\scripts\debug.ps1 events  [limit] [topics]     # 最近的后端事件
  .\scripts\debug.ps1 eval    "js 表达式"
  .\scripts\debug.ps1 screenshot [out.png]
  .\scripts\debug.ps1 stream  [topics]             # SSE 实时流（Ctrl+C 退出）
  .\scripts\debug.ps1 mcp     tools | call <tool> [json]

端口与 token 从 %APPDATA%\ding-ssh\debug.json 读取（token 每次启动应用都会变化）。
需要在应用内开启：设置 → 调试模式 → 启用。
#>
[CmdletBinding()]
param(
  [Parameter(Position = 0)][string]$Cmd = 'health',
  [Parameter(Position = 1)][string]$A1,
  [Parameter(Position = 2)][string]$A2,
  [Parameter(Position = 3)][string]$A3,
  [switch]$Local,
  [switch]$Raw
)

$ErrorActionPreference = 'Stop'
$infoPath = Join-Path $env:APPDATA 'ding-ssh\debug.json'
if (-not (Test-Path $infoPath)) {
  Write-Error "未找到 $infoPath —— 请先在应用里打开 设置 → 调试模式 → 启用。"
}
$cfg = Get-Content $infoPath -Raw -Encoding UTF8 | ConvertFrom-Json
if (-not $cfg.url) { Write-Error '调试服务未运行（debug.json 里没有 url）。' }
$script:Base = $cfg.url
$script:Auth = @{ Authorization = "Bearer $($cfg.token)" }

function Invoke-DebugApi {
  param([string]$Method, [string]$Path, $Body)
  $p = @{ Uri = "$script:Base$Path"; Headers = $script:Auth; Method = $Method; TimeoutSec = 40; UseBasicParsing = $true }
  if ($null -ne $Body) {
    $p.ContentType = 'application/json'
    $p.Body = ($Body | ConvertTo-Json -Compress -Depth 12)
  }
  $resp = Invoke-WebRequest @p
  if ($Raw) { return $resp.Content }
  try { return ($resp.Content | ConvertFrom-Json) } catch { return $resp.Content }
}

function Show($v) { if ($Raw) { $v } else { $v | ConvertTo-Json -Depth 12 } }

function Invoke-Mcp {
  param([string]$Method, $Params, [int]$Id = 1)
  $body = @{ jsonrpc = '2.0'; id = $Id; method = $Method }
  if ($null -ne $Params) { $body.params = $Params }
  Invoke-DebugApi -Method Post -Path '/mcp' -Body $body
}

switch ($Cmd.ToLower()) {
  'health'      { Show (Invoke-DebugApi GET '/v1/health') }
  'state'       { Show (Invoke-DebugApi GET '/v1/state') }
  'terminals'   { Show (Invoke-DebugApi GET '/v1/terminals') }
  'servers'     { Show (Invoke-DebugApi GET '/v1/servers') }
  'settings'    { Show (Invoke-DebugApi GET '/v1/settings') }
  'buffer'      { Show (Invoke-DebugApi GET "/v1/terminals/$A1/buffer?from=tail&lines=$(if ($A2) { $A2 } else { 200 })") }
  'input'       { Show (Invoke-DebugApi POST "/v1/terminals/$A1/input" @{ data = $A2 }) }
  'scroll'      { Show (Invoke-DebugApi POST "/v1/terminals/$A1/scroll" @{ to = $A2; value = [int]$A3 }) }
  'resize'      { Show (Invoke-DebugApi POST "/v1/terminals/$A1/resize" @{ cols = [int]$A2; rows = [int]$A3 }) }
  'open'        {
    if ($Local) { Show (Invoke-DebugApi POST '/v1/tabs' @{ local = $true }) }
    else { Show (Invoke-DebugApi POST '/v1/tabs' @{ serverId = $A1 }) }
  }
  'close'       { Show (Invoke-DebugApi DELETE "/v1/tabs/$A1") }
  'reconnect'   { Show (Invoke-DebugApi POST "/v1/sessions/$A1/reconnect" @{}) }
  'disconnect'  { Show (Invoke-DebugApi POST "/v1/sessions/$A1/disconnect" @{}) }
  'eval'        { Show (Invoke-DebugApi POST '/v1/eval' @{ js = $A1 }) }
  'events'      {
    $limit = if ($A1) { [int]$A1 } else { 50 }
    $q = if ($A2) { "?limit=$limit&topics=$A2" } else { "?limit=$limit" }
    Show (Invoke-DebugApi GET "/v1/events$q")
  }
  'screenshot'  {
    $out = if ($A1) { $A1 } else { Join-Path $PWD 'ding-ssh-screenshot.png' }
    Invoke-WebRequest -Uri "$script:Base/v1/screenshot" -Headers $script:Auth -OutFile $out -TimeoutSec 40 -UseBasicParsing
    "已保存: $out"
  }
  'stream'      {
    $topics = if ($A1) { $A1 } else { 'ssh.output,ssh.status' }
    "订阅 SSE：$topics（Ctrl+C 退出）"
    & curl.exe -N -H "Authorization: Bearer $($cfg.token)" "$script:Base/v1/stream?topics=$topics"
  }
  'mcp'         {
    switch ($A1) {
      'tools' { Show (Invoke-Mcp -Method 'tools/list' -Params @{}) }
      'resources' { Show (Invoke-Mcp -Method 'resources/list' -Params @{}) }
      'call'  {
        $args = if ($A3) { $A3 | ConvertFrom-Json } else { @{} }
        Show (Invoke-Mcp -Method 'tools/call' -Params @{ name = $A2; arguments = $args })
      }
      default { Show (Invoke-Mcp -Method 'initialize' -Params @{ protocolVersion = '2025-03-26'; capabilities = @{}; clientInfo = @{ name = 'debug.ps1'; version = '1.0' } }) }
    }
  }
  default { Write-Host "未知命令: $Cmd（可用：health/state/terminals/servers/settings/buffer/input/scroll/resize/open/close/reconnect/disconnect/eval/events/screenshot/stream/mcp）" }
}
