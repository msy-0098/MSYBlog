# Phase 3 Backend: DB-IP Lite and Trusted Client IP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 使用本地 DB-IP Lite MMDB 为管理员提供 IPv4/IPv6 归属地查询，并修复代理头无条件信任问题。

**Architecture:** 独立 ClientIPResolver 负责可信代理边界，GeoIP Reader 只读取本地 City/ASN MMDB，Service 负责地址分类与 DTO，Handler 仅接受单个 IP。查询校验错误通过上下文标记排除攻击失败计数，但仍保留访问日志。

**Tech Stack:** Go 1.25、Gin、net/netip、maxminddb-golang、DB-IP Lite City/ASN MMDB、GORM/PostgreSQL。

---

**配套前端计划：** frontend/docs/superpowers/plans/2026-07-23-phase-3-geoip-frontend.md。后端接口先完成。

### Task 1: 建立可信代理 ClientIPResolver

**Files:**
- Create: internal/middleware/client_ip.go
- Create: internal/middleware/client_ip_test.go
- Modify: internal/config/config.go
- Modify: internal/config/config_test.go
- Modify: config.yaml
- Modify: internal/middleware/access.go
- Modify: internal/middleware/ratelimit.go
- Modify: internal/handler/blog_handler.go

- [ ] **Step 1: 写伪造代理头红灯测试**

~~~go
func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
    resolver := middleware.MustNewClientIPResolver([]string{"127.0.0.1/32"})
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    req.RemoteAddr = "203.0.113.10:4567"
    req.Header.Set("X-Forwarded-For", "8.8.8.8")
    require.Equal(t, "203.0.113.10", resolver.Resolve(req).Client.String())
}
~~~

同时覆盖可信回环 Nginx、X-Real-IP、多级 X-Forwarded-For 从右向左剥离可信代理、非法头回退 RemoteAddr。

- [ ] **Step 2: 运行红灯测试**

Run: go test ./internal/middleware ./internal/config

Expected: FAIL，Resolver 尚不存在。

- [ ] **Step 3: 实现 Resolver 并替换调用点**

~~~go
type ClientIPResolver struct { trusted []netip.Prefix }

type ResolvedClientIP struct {
    Client netip.Addr
    Peer   netip.Addr
    Source string
}

func (r *ClientIPResolver) Resolve(req *http.Request) ResolvedClientIP
~~~

access、ratelimit 和文章点赞统一注入 Resolver，不再调用无条件信任 Header 的旧 ClientIP()。默认可信网段只含回环，生产额外网段必须显式配置。

- [ ] **Step 4: 运行测试、提交并双推送**

Run: go test ./internal/middleware ./internal/handler ./internal/config

Expected: PASS。

~~~powershell
git status --short
git add config.yaml internal/config/config.go internal/config/config_test.go internal/middleware/client_ip.go internal/middleware/client_ip_test.go internal/middleware/access.go internal/middleware/ratelimit.go internal/handler/blog_handler.go
git diff --cached --check
git commit -m "fix(security): trust forwarded IPs only from proxies"
git push origin master
git push gitee master
~~~

### Task 2: 加载 DB-IP Lite City/ASN 数据库

**Files:**
- Create: internal/geoip/record.go
- Create: internal/geoip/reader.go
- Create: internal/geoip/reader_test.go
- Create: data/geoip/.gitkeep
- Modify: go.mod
- Modify: go.sum
- Modify: .gitignore
- Modify: internal/config/config.go
- Modify: internal/config/config_test.go
- Modify: config.yaml

- [ ] **Step 1: 加依赖并写缺库、命中、关闭测试**

Run: go get github.com/oschwald/maxminddb-golang/v2

~~~go
func TestReaderReportsUnavailableWhenFilesMissing(t *testing.T) {
    reader, err := geoip.Open(geoip.Config{CityPath:"missing-city.mmdb", ASNPath:"missing-asn.mmdb"})
    require.Error(t, err)
    require.Nil(t, reader)
}
~~~

测试 fixture 使用测试目录中的最小 MMDB 或 writer 动态生成，不能把生产 DB-IP 数据提交到仓库。

- [ ] **Step 2: 运行红灯测试**

Run: go test ./internal/geoip ./internal/config

Expected: FAIL。

- [ ] **Step 3: 实现 Reader 和状态**

~~~go
type Record struct {
    CountryCode string
    CountryName string
    RegionName string
    CityName string
    Latitude float64
    Longitude float64
    ASN uint
    Organization string
}

type DatabaseStatus struct {
    Available bool       `json:"available"`
    Source string        `json:"source"`
    UpdatedAt *time.Time `json:"updatedAt"`
}
~~~

Reader.Lookup(netip.Addr) 同时查询 City 与 ASN；启动时文件缺失应记录可诊断警告并让查询 API 返回“IP 数据库暂不可用”，不能导致整个博客无法启动。.gitignore 必须忽略 data/geoip/*.mmdb，仅保留 .gitkeep。

- [ ] **Step 4: 运行测试、提交并双推送**

Run: go test ./internal/geoip ./internal/config

Expected: PASS。

~~~powershell
git status --short
git add go.mod go.sum .gitignore config.yaml internal/config internal/geoip data/geoip/.gitkeep
git diff --cached --name-status
git diff --cached --check
git commit -m "feat(geoip): load local db-ip lite databases"
git push origin master
git push gitee master
~~~

### Task 3: 实现管理员 IP 查询 API 与失败计数排除

**Files:**
- Create: internal/service/ip_lookup_service.go
- Create: internal/service/ip_lookup_service_test.go
- Create: internal/handler/admin_ip_lookup_handler.go
- Create: internal/router/admin_ip_lookup_routes_test.go
- Create: internal/middleware/access_test.go
- Modify: internal/middleware/access.go
- Modify: internal/router/router.go

- [ ] **Step 1: 写特殊地址与权限红灯测试**

~~~go
func TestIPLookupRejectsNonPublicAddresses(t *testing.T) {
    for _, input := range []string{"127.0.0.1", "10.0.0.1", "::1", "fe80::1", "224.0.0.1", "0.0.0.0"} {
        _, err := service.Lookup(context.Background(), input)
        require.ErrorIs(t, err, service.ErrNonPublicIP, input)
    }
}
~~~

Router 测试覆盖无管理员身份 401、普通格式错误 400、未命中 200、数据库不可用 503、命中 200。

- [ ] **Step 2: 运行红灯测试**

Run: go test ./internal/service ./internal/middleware ./internal/router -run 'IPLookup|AccessTracker'

Expected: FAIL。

- [ ] **Step 3: 实现严格输入与 DTO**

~~~go
type IPLookupResult struct {
    IP string                    `json:"ip"`
    Family string                `json:"family"`
    CountryCode string           `json:"countryCode"`
    Country string               `json:"country"`
    Region string                `json:"region"`
    City string                  `json:"city"`
    Latitude float64             `json:"latitude"`
    Longitude float64            `json:"longitude"`
    ASN uint                     `json:"asn"`
    Organization string          `json:"organization"`
    Source string                `json:"source"`
    DatabaseUpdatedAt *time.Time `json:"databaseUpdatedAt"`
    Found bool                   `json:"found"`
}
~~~

只接受 netip.ParseAddr 成功的单个 IPv4/IPv6；拒绝 URL、CIDR、端口、主机名、IPv6 zone、私网、回环、链路本地、组播、未指定和保留地址。

新增 POST /api/admin/ip-lookup，请求 JSON 为 {"ip":"8.8.8.8"}。

- [ ] **Step 4: 排除查询校验失败的自动封禁计数**

~~~go
const skipFailureCountKey = "access.skipFailureCount"
func SkipAccessFailureCount(c *gin.Context) { c.Set(skipFailureCountKey, true) }
~~~

IP Lookup Handler 在返回 400 前调用该函数；AccessTracker 仍写访问日志，但不增加失败计数，不触发自动封禁。

- [ ] **Step 5: 测试、提交并双推送**

Run: go test ./internal/service ./internal/middleware ./internal/handler ./internal/router

Expected: PASS。

~~~powershell
git status --short
git add internal/service/ip_lookup_service.go internal/service/ip_lookup_service_test.go internal/handler/admin_ip_lookup_handler.go internal/router/admin_ip_lookup_routes_test.go internal/middleware/access.go internal/middleware/access_test.go internal/router/router.go
git diff --cached --check
git commit -m "feat(admin): add safe ip location lookup"
git push origin master
git push gitee master
~~~

### Task 4: 提供 MMDB 更新命令与阶段验证

**Files:**
- Create: cmd/update-dbip/main.go
- Create: cmd/update-dbip/main_test.go
- Create: docs/dbip-update.md

- [ ] **Step 1: 写下载校验红灯测试**

测试命令下载到临时文件，验证 HTTP 200、非空、可被 maxminddb 打开后再原子替换目标文件；失败保留旧库。

~~~go
require.FileExists(t, oldPath)
require.Equal(t, oldBytes, mustRead(t, oldPath))
~~~

- [ ] **Step 2: 运行红灯测试**

Run: `go test ./cmd/update-dbip -run UpdateDBIP`

Expected: FAIL，安全下载和原子替换命令尚不存在。

- [ ] **Step 3: 实现安全更新流程**

命令参数为 -city-url、-asn-url、-dest；URL 仅允许 HTTPS，目标必须位于配置的 geoip 目录。文档给出服务器上由 systemd timer 或 cron 每月运行的示例，但不自动修改生产定时任务，也不写入任何令牌。

- [ ] **Step 4: 阶段验证、提交并双推送**

~~~powershell
go test ./internal/geoip ./internal/service ./internal/middleware ./internal/handler ./internal/router ./cmd/update-dbip
go build ./cmd/server
go build ./cmd/update-dbip
git status --short
git add cmd/update-dbip docs/dbip-update.md
git diff --cached --check
git commit -m "chore(geoip): add atomic db-ip updater"
git push origin master
git push gitee master
~~~

Expected: PASS；所有 *.mmdb 保持忽略状态。
