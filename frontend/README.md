# Sunshine Web Tool Frontend

Sunshine 代码生成工具的 Web 界面，基于 Vue 3 + Vite + Element Plus 构建。

## 功能特性

- 左侧导航菜单（SQL、Protobuf、Public 三大类）
- 数据库连接管理（MySQL）
- 表结构加载与选择
- 代码生成配置（模块名、服务名、项目名等）
- 文件上传（.proto、.yml/.yaml）
- 代码预览
- 代码生成与下载

## 开发环境

### 环境要求

- Node.js >= 16.0.0
- npm 或 pnpm

### 安装依赖

使用 npm:
```bash
cd frontend
npm install
```

使用 pnpm:
```bash
cd frontend
pnpm install
```

### 启动开发服务器

使用 npm:
```bash
npm run dev
```

使用 pnpm:
```bash
pnpm run dev
```

开发服务器启动后，访问 `http://localhost:5173`，API 请求会自动代理到后端 `http://localhost:24631`。

## 构建打包

### 构建生产版本

使用 npm:
```bash
npm run build
```

使用 pnpm:
```bash
pnpm run build
```

构建流程：
1. `vite build` - Vite 编译 Vue 组件并输出到 `../cmd/sunshine/server/static`
2. `node scripts/post-build.js` - 自动修补 `index.html`，添加 `/static/` 路径前缀和 `appConfig.js` 引用

构建完成后，产物会输出到 `../cmd/sunshine/server/static` 目录，Go 程序会通过 `embed.FS` 将这些文件嵌入到二进制中。

### 构建产物结构

```
cmd/sunshine/server/static/
├── index.html              # 入口页面
├── favicon.png             # 网站图标
├── appConfig.js            # 配置文件（API地址）
└── assets/
    ├── index-xxxxx.css     # 样式文件
    └── index-xxxxx.js      # JS 文件
```

## 部署方式

### 方式一：嵌入 Go 二进制（推荐）

1. 构建前端：
   ```bash
   cd frontend
   pnpm run build
   ```

2. 编译 Go 程序：
   ```bash
   cd ..
   go build -o sunshine-server ./cmd/sunshine/...
   ```

3. 运行：
   ```bash
   ./sunshine-server run
   ```

访问 `http://localhost:24631` 即可使用。

### 方式二：go run 直接运行（开发推荐）

```bash
cd ..
go run ./cmd/sunshine/main.go run
```

访问 `http://localhost:24631` 即可使用。

### 方式三：开发模式前后端分离

1. 启动后端：
   ```bash
   cd ..
   go run ./cmd/sunshine/main.go run
   ```

2. 启动前端开发服务器：
   ```bash
   cd frontend
   pnpm run dev
   ```

3. 访问 `http://localhost:5173` 进行开发调试。

## 配置说明

### appConfig.js

位于 `static/appConfig.js`，用于配置后端 API 地址：

```javascript
var appConfig = {
  sunshineServiceAddr: "http://localhost:24631/api/v1",
};
```

当使用非默认地址启动 Go 服务器时，Go 程序会自动将 `appConfig.js` 中的地址替换为实际地址。

## API 接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/api/v1/listDrivers` | GET | 获取数据库驱动列表 |
| `/api/v1/listTables` | POST | 获取数据库表列表 |
| `/api/v1/generate` | POST | 生成代码并下载 |
| `/api/v1/getTemplateInfo` | POST | 预览生成的代码 |
| `/api/v1/uploadFiles` | POST | 上传文件 |
| `/api/v1/record/:path` | GET | 获取历史记录 |

## 技术栈

- **Vue 3** - 前端框架
- **Vite** - 构建工具
- **Element Plus** - UI 组件库
- **Axios** - HTTP 客户端