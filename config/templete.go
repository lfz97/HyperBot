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

# Chrome MCP 配置 (需参考此项目进行配置 https://github.com/hangwin/mcp-chrome)
chromemcp:
  enabled: true
  mcptype: "streamable_http"
  mcpendpoint: "http://127.0.0.1:12306/mcp"
`
