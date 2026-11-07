# WearMemo

Wear OS 手表上的语音速记：抬腕说一句，异步转写，落进自建的 Blinko。

```
┌──────────────┐  multipart   ┌────────────────────────┐
│  Wear OS app │ ───────────► │  relay (Go, ARM64)     │
│  Kotlin      │              │                        │
│              │ ◄─────────── │  1. 收音频，202 + id    │
│ · 录音        │   202 + 轮询 │  2. 调 ASR              │
│ · 本地队列    │              │  3. 写 Blinko           │
│ · 状态回执    │              │  4. 删音频（除失败）     │
└──────────────┘              └────────────────────────┘
                                   │            │
                                   ▼            ▼
                             Qwen3-ASR      Blinko REST
```

## 为什么要有 relay

不是"技术上加不了"，而是四条硬约束：

1. **凭据不进手表。** Blinko 的 `apiToken` 是 `exp = 100 年`的 JWT（`server/lib/helper.ts:172`），丢了等于账号失守；ASR 的 key 同理。
2. **手表会睡。** ASR 在 ARM CPU 上要 15–60 秒，手表撑不住一条那么长的连接。relay 先返 `202`，结果靠轮询。
3. **需要幂等。** "上传成功但写 Blinko 失败"重试会变成两条笔记，必须服务端用客户端 UUID 去重。
4. **两个故障域。** 手表→ASR→Blinko 是三段独立的重试策略，集中在一处才好处理。

## Blinko 集成事实（已在本仓库核过）

| 用途 | 接口 |
|---|---|
| 写入 | `POST /api/v1/note/upsert`，字段 `content / type / attachments / references / metadata` |
| 读取 | `POST /api/v1/note/list`，`page / size / searchText` |
| 附件 | `POST /api/file/upload`（multipart `file`）→ `{path,type,size}` |
| 鉴权 | `Authorization: Bearer <apiToken>` |

- **tag 不需要额外接口**：正文里的 `#工作` 会被 `extractHashtags` 自动解析建标签。
- **双链是 `references: number[]`（笔记 ID）**，不支持 `[[标题]]` 语法 —— 这是"手表上不做引用"的技术原因。
- `NoteType`: `0=BLINKO, 1=NOTE, 2=TODO`，本项目固定用 `1`。
- `metadata` 是自由 JSON，用来存 `{clientId, asrModel, latencyMs, quality}` 供审计。

## ASR

不是 vLLM，是 **Ollama**：`frozenlab/qwen3-asr:1.7b`（GGUF Q8_0，qwen3vl 家族）。
音频走 `images` 字段 base64，prompt 留空：

```bash
curl $ASR_BASE/api/chat -d '{
  "model": "frozenlab/qwen3-asr:1.7b",
  "stream": false,
  "keep_alive": "30m",
  "messages": [{"role":"user","content":"","images":["<base64 wav>"]}]
}'
```

**实测延迟**（本机 → 你的 ASR 主机）：

| 音频 | 墙钟 | 倍率 | 说明 |
|---|---|---|---|
| 6.41s | 4.13s | 0.65x | 模型热 |
| 24.12s | 18.62s | 0.77x | prompt_eval 占 10.3s |
| 6.41s | 16.47s | — | 冷启动，其中 9.15s 是加载模型 |

结论：**1.7B 够快，不需要换 0.6B**。用 `keep_alive=30m` 把冷启动摊掉。

输出格式是 `language Chinese<asr_text>正文`，relay 会解析出语言和正文。

### 两个必须有的闸

静音输入**不会**返回空，而是返回一段流畅的幻觉文本。实测里模拟器静音 4 秒，模型吐出：

> El 20 de enero de 2018, el presidente de la República, Enrique Peña Nieto…

这条差点被写进 Blinko。所以 relay 有两道前置校验：

1. **静音闸**：调用前算 WAV 的 RMS，低于 `ASR_MIN_RMS`（默认 200）直接失败，不花 ASR 时间。
2. **语种闸**：识别出的语言不在 `ASR_LANGS`（默认 `Chinese,English`）里，判定为无语音。

两道闸都保留音频、不写 Blinko，失败原因写进 job 的 `error` 字段。

## 构建

```bash
# 需要 JDK 21+ 与 Android SDK（compileSdk 36）
./gradlew :app:assembleDebug

# 指定 relay 地址（默认 http://10.0.2.2:8080，即模拟器访问宿主机）
./gradlew :app:assembleDebug -PrelayBaseUrl=http://192.168.1.20:8080
```

debug 包允许明文 HTTP，方便直连局域网；release 包保持 Android 默认策略，
必须走 HTTPS（例如 Cloudflare Tunnel）。

CI 产物：`Actions → CI → wearmemo-debug-apk`。也可以在 `Run workflow` 时填
`relay_base_url`，直接产出一个指向你内网地址的包。

relay 镜像：`ghcr.io/<owner>/wearmemo-relay`，同时提供 `linux/arm64` 与 `linux/amd64`。

## 当前状态

- [x] 需求对齐（`REQUIREMENTS.md`）
- [x] 产品建议（`PRODUCT-ADVICE.md`）
- [x] 8 屏原型（`prototype/`，227dp 画布，`preview.png` 总览）
- [x] Android 骨架：录音、本地队列、轮询、6 个界面 —— `assembleDebug` 通过，已在 Wear OS 模拟器（454×454）跑起来
- [x] relay（Go）：Web 配置页 + 配对码 + 每设备令牌（可吊销）、UUID 幂等、单 worker、重启恢复、静音/语种双闸
- [x] **端到端跑通**：配置 → 配对 → 手表上传 → ASR → Blinko，已验证中文与粤语
- [ ] 手表端配对界面（第 7 屏：输入 6 位码）+ DataStore 存令牌
- [ ] 真机联调（模拟器麦克风是静音，只验到了失败路径）
- [ ] 前台服务保活 + 息屏继续录 + 60 秒自动分段
- [ ] Tile / 表盘复杂功能
- [ ] 通知回执（v1 不引 FCM，失败态以角标呈现）

### 部署与配对

```bash
cp relay.env.example .env   # 只需要填 RELAY_ADMIN_TOKEN
docker compose up -d --build
```

然后打开 `https://你的域名/`：

1. **首次配置** —— 填 ASR 地址/模型、Blinko 地址/Token，保存（存在 `data/settings.json`，改完立刻生效，不用重启容器）。
2. **生成配对码** —— 得到一个 6 位码（Crockford Base32，去掉了 0/O/1/I/L），5 分钟内有效、只能用一次。
3. **手表输入这串码** —— 换到设备令牌，之后所有请求都带它。
4. **设备页** —— 看谁在用、最近一次请求时间、随时吊销。

relay 地址在编译期烤进 APK（你自己构建，所以合理）：

```bash
./gradlew :app:assembleDebug -PrelayBaseUrl=https://memo.example.com
```

这样手表运行时就只剩"输入配对码"一件事。

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/` | — | 未配置跳 `/setup`，否则跳 `/admin` |
| POST | `/v1/pair` | — | 手表用配对码换设备令牌，一次性 |
| POST | `/v1/memos` | 设备令牌 | multipart：`clientId` + `audio`，返回 `202 {id}` |
| GET | `/v1/memos/{id}` | 设备令牌 | 轮询：`status / text / language / noteId / error` |
| GET | `/healthz` | — | 存活探针 |

令牌是 opaque 的 `wm_` 前缀随机串，服务端只存 sha256——**不用 JWT**，因为手表时钟可能不准，
`exp` 会随机失效。

## 设计

纯黑底 + 灰度层级，只有三个语义色：`#FF453A` 录制、`#30D158` 成功、`#FF9F0A` 待处理。
排版按 227dp 画布（Wear OS 454px @ 2.0 密度）设计，正文 13sp，主操作 96dp。
