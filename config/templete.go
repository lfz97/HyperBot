package config

const Template string = `# 模型配置
model:
  model: "deepseek-reasoner"
  baseurl: "https://api.deepseek.com"
  apikey: ""
  apitype: "openai"

# 博查 MCP 配置
bochamcp:
  enabled: false
  apikey: ""
  mcptype: "streamable_http"
  mcpendpoint: "https://mcp.bochaai.com/mcp"

# MCP Exec 配置 (shell mcp工具，项目地址：https://github.com/lfz97/mcp-exec)
mcpexec:
  enabled: false
  mcptype: "streamable_http"
  mcpendpoint: 

# Chrome MCP 配置 (需要安装chrome和nodejs)
chromemcp:
  enabled: false
  mcptype: "stdio"
  command: "npx"
  args:
    - "-y"
    - "chrome-devtools-mcp@latest"
    - "--slim"
    - "--headless"
  exitcommand: 'powershell -Command Get-CimInstance Win32_Process -Filter "CommandLine like ''%chrome-devtools-mcp%''" | Stop-Process -Id { $_.ProcessId } -Force'
`
