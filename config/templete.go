package config

const Template string = `# 模型配置
model:
  model: "deepseek-reasoner"
  baseurl: "https://api.deepseek.com"
  apikey: "your-api-key"
  apitype: "openai" # openai 或者 anthropic

# 博查 MCP 配置
bochamcp:
  enabled: false
  apikey: "your-api-key"
  mcptype: "streamable_http"
  mcpendpoint: "https://mcp.bochaai.com/mcp"

# MCP Exec 配置 (shell mcp工具，项目地址：https://github.com/lfz97/mcp-exec)
mcpexec:
  enabled: false
  mcptype: "streamable_http"
  mcpendpoint: "endpoint" #   例如 "http://1.2.3.4:8080/mcp"

# Chrome MCP 配置 (需参考此项目进行配置 https://github.com/hangwin/mcp-chrome 重要：如果要开始新对话，请在chrome重启mcp端点，否则会报错)
chromemcp:
  enabled: false
  mcptype: "streamable_http"
  mcpendpoint: "http://127.0.0.1:12306/mcp"
`
