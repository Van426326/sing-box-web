## 查询Routings配置
curl 'http://192.168.10.200:2023/graphql' \
  -H 'Accept-Language: zh-CN,zh;q=0.9' \
  -H 'Connection: keep-alive' \
  -b 'agh_session=<optional-session-cookie>' \
  -H 'Origin: http://192.168.10.200:2023' \
  -H 'Referer: http://192.168.10.200:2023/' \
  -H 'User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36' \
  -H 'accept: application/graphql-response+json, application/json' \
  -H 'authorization: Bearer <daed-token>' \
  -H 'content-type: application/json' \
  --data-raw '{"query":"query Routings {\n  routings {\n    id\n    name\n    selected\n    routing {\n      string\n    }\n  }\n}","operationName":"Routings"}' \
  --insecure


## 修改Routings配置
  curl 'http://192.168.10.200:2023/graphql' \
  -H 'Accept-Language: zh-CN,zh;q=0.9' \
  -H 'Connection: keep-alive' \
  -b 'agh_session=<optional-session-cookie>' \
  -H 'Origin: http://192.168.10.200:2023' \
  -H 'Referer: http://192.168.10.200:2023/' \
  -H 'User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36' \
  -H 'accept: application/graphql-response+json, application/json' \
  -H 'authorization: Bearer <daed-token>' \
  -H 'content-type: application/json' \
  --data-raw $'{"query":"mutation UpdateRouting($id: ID\u0021, $routing: String\u0021) {\\n  updateRouting(id: $id, routing: $routing) {\\n    id\\n  }\\n}","variables":{"id":"Y3Vyc29yMQ","routing":"pname(mosdns) -> must_rules\\npname(AdGuardHome) -> must_rules\\n\\n# 代理 DNS\\ndip(8.8.8.8/32) -> proxy\\ndip(8.8.4.4/32) -> proxy\\ndip(1.1.1.1/32) -> proxy\\ndip(1.0.0.1/32) -> proxy\\n\\n# 直连 DNS\\ndip(106.55.91.174/32) -> direct\\ndip(223.5.5.5/32) -> direct\\ndip(180.184.1.1/32) -> direct\\ndip(192.168.10.1/32) -> direct\\n\\n# 本地主机中的网络管理器应该是直接的，以避免在绑定到WAN\\npname(NetworkManager, systemd-resolved, dnsmasq) -> must_direct\\n#pname(mosdns) && l4proto(udp) && dport(5335) -> direct(must)\\n#pname(AdGuardHome) && dport(53) -> direct(must)\\npname(ql) -> direct\\ndscp(4) -> direct\\n\\n# 家 & kt\\ndip(\\n192.168.2.0/24,\\n10.1.19.0/24,\\n10.5.210.0/24,\\n10.5.220.0/24,\\n10.7.0.0/24,\\n10.9.6.0/24,\\n10.9.9.0/24,\\n10.10.1.0/24,\\n10.10.20.0/24,\\n10.16.18.2/32,\\n10.28.8.0/24,\\n10.102.0.0/24,\\n10.118.0.0/24,\\n10.170.13.0/24,\\n10.180.182.182/32,\\n10.200.40.0/24,\\n10.206.0.0/24,\\n10.233.80.0/16,\\n11.1.0.0/16,\\n92.168.0.0/16,\\n100.100.121.0/24,\\n116.62.134.9/32,\\n130.130.8.0/24,\\n130.150.0.0/16,\\n172.16.1.0/24,\\n172.16.63.0/24,\\n172.16.129.0/24,\\n172.16.131.0/24,\\n172.17.20.0/24,\\n172.17.88.0/24,\\n172.20.1.0/24,\\n172.21.2.0/24,\\n172.28.100.0/24,\\n172.28.250.0/24,\\n172.31.6.0/24,\\n172.168.4.0/24,\\n172.168.40.0/24,\\n172.168.60.0/24,\\n180.97.234.247/32,\\n192.2.2.0/24,\\n192.144.201.0/24,\\n192.168.0.20/32,\\n192.168.0.31/32,\\n192.168.0.111/32,\\n192.168.0.112/32,\\n192.168.7.30/32,\\n192.168.48.0/24,\\n192.168.52.0/24,\\n192.168.54.0/24,\\n192.168.98.0/23,\\n192.168.144.0/24,\\n192.168.146.0/23,\\n192.168.148.0/24,\\n192.168.162.0/24,\\n193.169.16.0/24,\\n195.165.35.0/24,\\n196.4.22.0/24,\\n200.200.200.0/24,\\n172.16.40.196/32,\\n192.168.0.218/32,\\n192.168.0.64/32,\\n192.168.0.191/32\\n) -> singbox\\n\\n### 禁用Quic，避免CPU高负载及内存泄露\\n#l4proto(udp) && dport(443) -> block\\n#domain(geosite:category-ads) -> block\\n#domain(geosite:category-ads-all) -> block\\n\\n# 订阅直连防止回环\\ndomain(suffix: akri.top) -> direct\\n\\n# steam\\ndomain(\\nkeyword: \\nsteamserver.net,\\nsteamgames.com,\\nsteamusercontent.com,\\nsteamcdn-a.akamaihd.net,\\nsteamstat.us\\n) -> direct\\n\\ndomain(\\nsuffix: \\nsteamcommunity.com,\\nsteamcontent.com,\\nsteamstatic.com\\n) -> proxy\\n\\ndomain(\\nkeyword: \\nsteam-chat.com,\\napi.steampowered.com,\\nstore.steampowered.com\\n) -> proxy\\n\\n# LAN & Private Check\\n# 将其放在最前面，以防止广播，多播和其他应发送到局域网的数据包被代理转发\\ndip(224.0.0.0/3, \'ff00::/8\') -> direct\\ndip(geoip:private) -> direct\\ndip(104.224.154.20) -> direct\\n\\n### 微信 & 腾讯优化 (高优先级 - Direct)\\ndomain(suffix: cdn-go.cn) -> direct\\ndomain(suffix: smtcdns.com) -> direct\\ndomain(suffix: smtcdns.net) -> direct\\ndomain(geosite:tencent) -> direct\\n\\n### CN Direct Services\\ndomain(geosite:alibaba) -> direct\\n#domain(geosite:apple@cn) -> direct\\ndomain(geosite:microsoft@cn) -> direct\\ndomain(geosite:steam@cn) -> direct\\ndomain(suffix: cm.steampowered.com) -> direct\\ndomain(suffix: steamserver.net) -> direct\\n\\n### Proxy Services Group\\n# AI\\ndomain(geosite:openai) -> proxy\\n#domain(suffix: anthropic.com) -> proxy\\n#domain(suffix: claude.ai) -> proxy\\ndomain(suffix: z.ai) -> proxy\\n\\n\\n# ============================================================\\n# Claude AI 完整代理规则 (dae Format)\\n# Updated: 2026-04-10\\n# ============================================================\\n\\n# ======== 核心域名 ========\\ndomain(suffix: claude.ai) -> proxy\\ndomain(suffix: anthropic.com) -> proxy\\ndomain(suffix: claude.com) -> proxy\\ndomain(suffix: claudeusercontent.com) -> proxy\\ndomain(suffix: claudemcpclient.com) -> proxy\\n\\n# ======== CDN & 静态资源 ========\\ndomain(full: servd-anthropic-website.b-cdn.net) -> proxy\\ndomain(full: cdn.usefathom.com) -> proxy\\n\\n# ======== 监控/遥测：Datadog RUM ========\\ndomain(full: browser-intake-us5-datadoghq.com) -> proxy\\ndomain(keyword: datadoghq) -> proxy\\n\\n# ======== 客服：Intercom ========\\ndomain(suffix: intercom.io) -> proxy\\ndomain(suffix: intercomcdn.com) -> proxy\\n\\n# ======== Feature Flags：Statsig ========\\ndomain(suffix: statsigapi.net) -> proxy\\n\\n# ======== 错误追踪：Sentry ========\\ndomain(suffix: sentry.io) -> proxy\\n\\n# ======== Google 静态资源（页面字体等）========\\ndomain(full: t0.gstatic.com) -> proxy\\ndomain(full: t1.gstatic.com) -> proxy\\ndomain(full: t2.gstatic.com) -> proxy\\ndomain(full: t3.gstatic.com) -> proxy\\n\\n# ======== Claude Code 更新/下载 ========\\ndomain(full: storage.googleapis.com) -> proxy\\n\\n# ======== Anthropic 自有 IP 段 ========\\ndip(160.79.104.0/21) -> proxy\\ndip(\'2607:6bc0::/32\') -> proxy\\n# IP-ASN,399358 原生不支持直接匹配 ASN。如需匹配，需依赖自定义 DAT 文件，或直接省略此条。\\n\\n# ======== 关键字兜底（可选）========\\ndomain(keyword: claude) -> proxy\\ndomain(keyword: anthropic) -> proxy\\n\\n# Social & Media\\ndomain(geosite:telegram) -> proxy\\ndip(geoip:telegram) -> proxy\\ndomain(geosite:twitter) -> proxy\\ndomain(geosite:meta) -> proxy\\ndomain(geosite:tiktok) -> proxy\\ndomain(suffix: roovza-launches.appsflyersdk.com) -> proxy\\ndomain(geosite:netflix) -> proxy\\ndomain(geosite:disney) -> proxy\\ndomain(geosite:twitch) -> proxy\\n\\n# Dev & Tools\\ndomain(geosite:github) -> proxy\\ndomain(geosite:docker) -> proxy\\ndomain(suffix: gradle.org) -> proxy\\ndomain(suffix: linux.do) -> proxy\\n\\n# Google\\ndomain(geosite:google) -> proxy\\n\\n# Trading & Finance\\ndomain(keyword: tradingview) -> proxy\\n\\n### Final Fallbacks\\ndomain(geosite:geolocation-\u0021cn) -> proxy\\ndomain(geosite:cn) -> direct\\ndip(geoip:cn) -> direct\\n\\nfallback: proxy"},"operationName":"UpdateRouting"}' \
  --insecure

## 重载配置文件
  curl 'http://192.168.10.200:2023/graphql' \
  -H 'Accept-Language: zh-CN,zh;q=0.9' \
  -H 'Connection: keep-alive' \
  -b 'agh_session=<optional-session-cookie>' \
  -H 'Origin: http://192.168.10.200:2023' \
  -H 'Referer: http://192.168.10.200:2023/' \
  -H 'User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36' \
  -H 'accept: application/graphql-response+json, application/json' \
  -H 'authorization: Bearer <daed-token>' \
  -H 'content-type: application/json' \
  --data-raw $'{"query":"mutation Run($dry: Boolean\u0021) {\\n  run(dry: $dry)\\n}","variables":{"dry":false},"operationName":"Run"}' \
  --insecure
