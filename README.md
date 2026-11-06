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

Qwen3-ASR（`1.7B` / `0.6B`，**没有 0.7B**）。官方建议用 vLLM 起服务，走 OpenAI 兼容接口：

```bash
qwen-asr-serve Qwen/Qwen3-ASR-1.7B --gpu-memory-utilization 0.8 --host 0.0.0.0 --port 8000
```

- 模型名在 relay 里是**配置项**，1.7B ↔ 0.6B 只改一个字符串，架构不分叉。
- **官方没有置信度字段**（输出只有 `language` + `text`）。低置信度判定只能靠代理指标：VAD 语音占比、电平、时长/字数比，或 vLLM 的 `logprobs`（需实测）。
- `ai.speechToText` 在 Blinko 里是**被注释掉的空实现**，不能用。

## 构建

```bash
# 需要 JDK 21+ 与 Android SDK（compileSdk 36）
gradle :app:assembleDebug
```

relay 地址与设备令牌通过 `app/build.gradle.kts` 的 `buildConfigField` 注入：

```kotlin
buildConfigField("String", "RELAY_BASE_URL", "\"http://10.0.2.2:8080\"")
buildConfigField("String", "DEVICE_TOKEN", "\"dev-token-change-me\"")
```

## 当前状态

- [x] 需求对齐（`REQUIREMENTS.md`）
- [x] 产品建议（`PRODUCT-ADVICE.md`）
- [x] 8 屏原型（`prototype/`，227dp 画布，`preview.png` 总览）
- [x] Android 骨架：录音、本地队列、轮询、6 个界面 —— **`gradle :app:assembleDebug` 已通过**
- [x] relay（Go）：客户端 UUID 幂等、单 worker、重启恢复未完成任务、失败保留音频
      —— 已编译并用 `healthz` / 401 / 404 冒烟测试
- [ ] 端到端联调（真 ASR + 真 Blinko）
- [ ] 前台服务保活 + 息屏继续录 + 60 秒自动分段
- [ ] Tile / 表盘复杂功能
- [ ] 通知回执（决定 v1 不引 FCM，失败态以角标呈现）

### relay 的接口

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/memos` | multipart：`clientId` + `audio`，返回 `202 {id}`；同 `clientId` 重复提交返回同一个 job |
| GET | `/v1/memos/{id}` | 轮询：`{status: queued\|transcribing\|writing\|done\|failed, text, noteId, error}` |
| GET | `/healthz` | 存活探针 |

启动：

```bash
cp relay.env.example .env   # 填 BLINKO_TOKEN 与 RELAY_DEVICE_TOKEN
docker compose up -d --build
```

## 设计

纯黑底 + 灰度层级，只有三个语义色：`#FF453A` 录制、`#30D158` 成功、`#FF9F0A` 待处理。
排版按 227dp 画布（Wear OS 454px @ 2.0 密度）设计，正文 13sp，主操作 96dp。
