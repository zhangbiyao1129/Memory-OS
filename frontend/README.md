# Memory OS 前端管理台

Nuxt 3 静态构建，托管在 Nginx。

## 开发

```bash
npm install
npm run dev
```

## 构建

```bash
npm install
npm run build      # 或 npm run generate 生成静态站点
```

## 环境变量

- `NUXT_PUBLIC_API_BASE`：后端 API 地址，构建时注入（见 `nuxt.config.ts` 的 `runtimeConfig.public.apiBase`）

## 目录

- `pages/`：路由页面
- `components/`：Vue 组件
- `composables/`：组合式函数
- `stores/`：Pinia 状态
- `assets/`：静态资源
