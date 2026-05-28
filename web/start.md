在主目录 `D:\ideaCOde\cv\zbjj` 启动。

**后端**
```powershell
$env:HTTP_ADDR="127.0.0.1:8080"
$env:JWT_SECRET="local-demo-jwt-secret"
go run ./cmd/api
```

验证：
```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
```

**前端**
另开一个 PowerShell：

```powershell
cd web
$env:VITE_API_BASE="http://127.0.0.1:8080"
npm run dev -- --host 0.0.0.0 --port 5173
```

访问：

- 管理端：`http://127.0.0.1:5173/admin`
- 用户端：`http://127.0.0.1:5173/m`

账号：

- 管理员：`admin / admin123`
- 用户A：`userA / 123456`
- 用户B：`userB / 123456`
- 用户C：`userC / 123456`

如果 8080 或 5173 被占用，先停掉对应进程，或改端口。

## 依赖顺序

1. `docker compose up -d redis mysql`
2. 后端 `go run ./cmd/api`
3. 前端 `npm run dev`

如果登录或出价接口返回网络错误，先检查后端是否已启动；如果后端启动失败，先检查 Redis 容器是否健康。
