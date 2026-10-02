# RCUPC — Архитектура и технический дизайн

Документ описывает **целевую архитектуру** LAN remote-desktop на Go. Реализации здесь нет —
только решения, границы, потоки данных и обоснования. Каждый крупный выбор сопровождается
плюсами, минусами, задержкой, сложностью и пригодностью для LAN realtime.

---

## 1. Цели, принципы и границы

### 1.1. Что оптимизируем

1. **Задержка важнее пропускной способности.** В LAN много bandwidth, но latency и
   jitter ощущаются как «тормозит курсор». Поэтому: не накапливать кадры, не буферизовать
   лишнее, дробить работу, мерить каждый этап.
2. **Понятность важнее «магии».** Каждый компонент должен быть читаемым и заменяемым.
3. **Модульность.** Capture, encode, transport, decode, render, input — независимые,
   соединённые интерфейсами.
4. **Постепенность.** Каждая фаза roadmap даёт работающий результат, а не только фундамент.

### 1.2. Ключевые архитектурные принципы

- **Control plane / data plane разделены.** Управление и ввод — надёжные, упорядоченные,
  маленькие сообщения. Видео — поток больших, неупорядоченных, «лучше потерять, чем
  задержать» пакетов. Их требования противоположны, поэтому и каналы разные.
- **Latest-frame semantics.** Для видео очередь вредна: чем она длиннее, тем больше lag.
  Вместо очереди — «почтовый ящик» на один кадр: новый кадр вытесняет старый.
- **Платформенный код — за build tags.** `capture_dxgi_windows.go`, `capture_gdi_windows.go`,
  `input_x11_linux.go` и т.п. Ядро не знает про Windows API.
- **Интерфейсы на границах.** Меняем GDI↔DXGI, MJPEG↔H.264, TCP↔QUIC, не трогая остальное.
- **Zero-copy там, где дорого.** GPU-текстура захвата по возможности идёт прямо в encoder,
  без GPU→CPU→GPU. Каждая копия кадра на 1080p60 — это десятки МБ/с и лишние миллисекунды.
- **Измеримость.** Timestamp'ы на каждом этапе (capture → encode → send → recv → decode →
  render) складываются в end-to-end latency.
- **Безопасность проектируется с Phase 1, но реализуется на Phase 11.** Транспорт с самого
  начала умеет «завернуться» в защищённый канал, даже если сначала он выключен.

### 1.3. Явные границы проекта

- Только LAN. Никакого relay, cloud, STUN/TURN, NAT traversal.
- Host — Windows. Client — Linux. Кросс-платформенность не цель.
- GUI клиента — отдельное desktop-приложение, не web и не Electron.
- Никаких kernel-драйверов (виртуальный HID, драйвер захвата) — только user-mode API.

---

## 2. Архитектурная схема

### 2.1. Компоненты и потоки

```
                                   WINDOWS HOST
  ┌────────────────────────────────────────────────────────────────────────┐
  │                                                                        │
  │   capture/                     encode/                  audio/ (future) │
  │  ┌────────────┐   frame   ┌──────────────────┐  bits   ┌────────────┐   │
  │  │ DXGI / GDI │──────────▶│ MJPEG / H.264 HW │────────▶│ WASAPI     │   │
  │  │ Capturer   │  (slot)   │ Encoder          │         │ Capturer   │   │
  │  └─────▲──────┘           └────────┬─────────┘         └─────┬──────┘   │
  │        │                           │                         │          │
  │        │                           ▼                         ▼          │
  │  ┌─────┴───────────────────────────────────────────────────────────┐   │
  │  │                     host/session (Manager)                      │   │
  │  │  mux видео/audio/control (out)   │   demux input (in)           │   │
  │  │  backpressure: drop frame         │   приоритет ввода > видео   │   │
  │  └─────┬─────────────────────────────────────────────▲────────────┘   │
  │        │ packets                                     │ input events   │
  │        ▼                                             │                │
  │  ┌───────────────────┐                       ┌───────┴──────────────┐ │
  │  │ transport (out)   │                       │ input/ SendInput     │ │
  │  │ TCP │ UDP │ QUIC  │                       │ backend (user32)     │ │
  │  └─────────┬─────────┘                       └──────────────────────┘ │
  └────────────┼───────────────────────────────────────────────────────────┘
               │
               │   LAN  ──  control + input: надёжный канал
               │        └─  video: неупорядоченные datagrams
               │
  ┌────────────┼───────────────────────────────────────────────────────────┐
  │            ▼                                                           │
  │  ┌───────────────────┐                       ┌──────────────────────┐  │
  │  │ transport (in)    │                       │ input capture        │  │
  │  │ demux + jitter    │                       │ X11 / (Wayland позже)│  │
  │  └─────────┬─────────┘                       └──────────┬───────────┘  │
  │            │ packets                                     │ events       │
  │            ▼                                             ▼              │
  │  ┌──────────────────────────────────────────────────────────────────┐  │
  │  │                      client/session (Manager)                    │  │
  │  │  demux; latest-frame slot; input sender; stats                    │  │
  │  └─────────┬───────────────────────────────────────▲──────────────┘  │
  │            │ frame                                  │                │
  │            ▼                                        │                │
  │  ┌──────────────┐    frame     ┌──────────────────┐ │                │
  │  │ decode/      │─────────────▶│ render/          │ │                │
  │  │ H.264/MJPEG  │  (slot)      │ OpenGL / Ebiten  │─┘                │
  │  └──────────────┘              │ + input overlay  │                  │
  │                                └──────────────────┘                  │
  │                              LINUX CLIENT                             │
  └───────────────────────────────────────────────────────────────────────┘
```

### 2.2. Control plane vs data plane

| | Control plane | Data plane (video) |
|---|---|---|
| Трафик | handshake, caps, input, ping, error | видеокадры, (future audio) |
| Требования | надёжно, упорядоченно, доставка важнее задержки | задержка важнее доставки, порядок желателен |
| Типичный размер | десятки–сотни байт | десятки–сотни КБ на кадр |
| Частота | эпизодически | 30–144 Гц |
| Транспорт по умолчанию | TCP | UDP datagrams |
| При потере | retransmit | пропустить / дождаться keyframe |

Такое разделение — центральное решение всей архитектуры. Оно позволяет вводить UDP-видео
(Phase 6) и защищённый канал (Phase 11), не переписывая логику сессии.

---

## 3. Структура директорий

```
RCUPC/
├── go.mod
├── README.md
├── docs/
│   ├── ARCHITECTURE.md      # этот файл
│   ├── PROTOCOL.md
│   ├── SECURITY.md
│   └── ROADMAP.md
│
├── cmd/                     # entrypoints (тонкие, только wiring)
│   ├── rcupc-host/
│   │   └── main.go
│   └── rcupc-client/
│       └── main.go
│
├── pkg/                     # разделяемые, переиспользуемые пакеты
│   ├── protocol/            # wire format, message types, (de)serialization, framing
│   ├── transport/           # Transport interfaces + реализации (tcp, udp, quic)
│   ├── config/              # конфигурация host/client, defaults, загрузка
│   ├── logging/             # структурированный логгер, уровни, метрики
│   └── rerrors/             # типизированные ошибки, коды протокола
│
├── host/                    # всё, что исполняется на Windows
│   ├── capture/             # Capturer interface + dxgi/gdi implementations
│   ├── encode/              # Encoder interface + mjpeg/h264(MFT/venc) implementations
│   ├── input/               # инъекция ввода (SendInput)
│   ├── audio/               # (future) WASAPI capture + encode
│   ├── session/             # host-side session/connection manager, mux/demux
│   └── server/              # listener, accept loop, pairing, per-connection lifecycle
│
├── client/                  # всё, что исполняется на Linux
│   ├── decode/              # Decoder interface + ffmpeg/openh264 implementations
│   ├── render/              # Renderer interface + opengl/ebiten implementations
│   ├── input/               # захват локального ввода (X11, позже Wayland/libei)
│   ├── session/             # client-side session manager, jitter/latest-frame
│   └── app/                 # wiring клиента: окно, event loop, горячие клавиши
│
└── third_party/             # изолированные нативные shim'ы (cgo)
    ├── dxgi_capture/        # C/C++ обёртка D3D11 + IDXGIOutputDuplication
    ├── mf_encoder/          # C/C++ обёртка Media Foundation / NVENC / AMF
    └── ffmpeg_decode/       # C обёртка libavcodec (Linux decode)
```

Пояснения к выбору:

- `internal/` не используется: это не библиотека для внешних потребителей, а приложение,
  но разделение `pkg/` (shared) и `host/`+`client/` (исполняемое) делает границы очевидными.
  Если позже понадобится скрыть API — `pkg/*` можно перенести в `internal/`.
- `third_party/` намеренно отделён: там нативный код и cgo, и он не должен «протекать» в
  Go-архитектуру. Go-пакеты `host/capture` и `host/encode` оборачивают shim'ы.
- Платформенные файлы внутри пакетов выбираются build tags, а не отдельными папками:
  `host/input/sendinput_windows.go`, `client/input/x11_linux.go`.

---

## 4. Пакеты, модули и интерфейсы

Ниже — назначение, ответственность и **границы**, которые должны быть интерфейсами.
Сигнатуры приведены схематично (design-level), чтобы зафиксировать контракты, а не реализацию.

### 4.1. Shared

#### `pkg/protocol`
- **Роль:** единственное место, знающее байтовый формат RCUPC.
- **Отвечает за:** типы сообщений, заголовки, framing, (де)сериализацию, версионирование,
  capabilities, коды ошибок.
- **Не знает** про sockets, кадры, GPU.
- **Ключевые сущности:** `Header`, `Message` (interface), `Encoder`/`Decoder` сообщений,
  `Capabilities`, `ErrorCode`.
- **Почему интерфейс:** разные сообщения (control/video/input/audio) имеют разный payload,
  но общий заголовок; интерфейс `Message` позволяет мультиплексировать их.

#### `pkg/transport`
- **Роль:** доставка байт/сообщений. Не знает про смысл сообщений.
- **Интерфейсы:**
  - `Stream` для control/input: надёжный упорядоченный двунаправленный поток
    (`ReadMessage`/`WriteMessage` или `net.Conn`-подобный с framing наверху).
  - `DatagramConn` для видео: `WritePacket([]byte)`, `ReadPacket() ([]byte, …)`,
    с учётом MTU и без гарантии доставки.
- **Реализации:** `transport/tcp`, `transport/udp`, `transport/quic` (позже).
- **Почему интерфейс:** позволяет подменить TCP на QUIC/Noise, не трогая сессии.
- **Важно:** framing control-сообщений живёт в `protocol`, а `transport` лишь передаёт
  байты; но `transport` может владеть буфером и MTU-политикой для datagrams.

#### `pkg/config`
- Структуры конфигурации host и client, дефолты, загрузка из файла/флагов, валидация.
- Не содержит логики — только данные.

#### `pkg/logging`
- Обёртка над `log/slog`, уровни, структурированные поля (session, seq, latency).
- Метрики latency — сюда же или в отдельный `metrics` пакет.

#### `pkg/rerrors`
- Типизированные ошибки с кодами протокола, чтобы host и client одинаково их трактовали.

### 4.2. Host

#### `host/capture`
- **Роль:** получить кадр рабочего стола.
- **Интерфейс `Capturer`:**
  - `Start(cfg) error`
  - `Frames() <-chan *Frame` — канал «latest-frame» (буферизация ≤1)
  - `Stop()`
- **`Frame`:** ссылка на GPU-ресурс (D3D11 texture) **или** CPU-буфер + метаданные
  (width, height, stride, format, timestamp, dirty rects, monitor id).
- **Реализации:**
  - `capture/gdi_windows.go` — BitBlt, CPU-буфер. Просто, медленно, без GPU. Для Phase 3.
  - `capture/dxgi_windows.go` — Desktop Duplication, GPU-текстура. Основной путь.
  - `capture/wgc_windows.go` — Windows.Graphics.Capture (позже, опционально).
- **Почему интерфейс:** DXGI сложен и капризен (access lost, смена разрешения), а GDI даёт
  рабочий результат раньше. Интерфейс позволяет начать с GDI и перейти на DXGI.

#### `host/encode`
- **Роль:** превратить кадр в сжатый bitstream.
- **Интерфейс `Encoder`:**
  - `Open(params) error` (codec, resolution, bitrate, fps, rate control)
  - `Encode(*Frame) ([]Packet, error)` или входной канал/выходной канал
  - `RequestKeyframe()`, `Close()`
- **Реализации:**
  - `encode/mjpeg.go` — просто, низкая задержка, высокий bitrate. Для Phase 5.
  - `encode/h264_mf_windows.go` — Media Foundation hardware MFT (Intel/NVIDIA/AMD).
  - `encode/h264_nvenc.go` — прямой NVENC (опционально, если MF мало).
  - `encode/h264_amf.go`, `encode/h264_qsv.go` — по мере надобности.
- **Почему интерфейс:** encoder'ы сильно vendor-specific; нужно уметь выбирать по наличию
  GPU и сравнивать.

#### `host/input`
- **Роль:** инъекция mouse/keyboard в Windows.
- **Интерфейс `Backend`:** `Inject(Event) error`, `SupportedModes() Mode`.
- **`Event`:** mouse move (абсолют./относит.), button down/up, wheel, key down/up, unicode.
- **Реализация:** `sendinput_windows.go` через `SendInput` (user32). Чистый Go + `x/sys/windows`,
  cgo не нужен.
- **Ограничение (важно):** `SendInput` не работает на secure desktop (UAC/логин). Это
  документируется, но решается (если вообще) только service/UI-Access — вне текущих целей.

#### `host/audio` (future)
- `Capturer` (WASAPI loopback) + `Encoder` (Opus). Только интерфейсы на будущее.

#### `host/session`
- **Роль:** мозг одной сессии на host.
- **Отвечает за:**
  - demux входящих сообщений (control/input),
  - mux исходящих (video/audio/control),
  - handshake и version/caps negotiation вместе с `protocol`,
  - backpressure: если transport не успевает — дропать видео-кадры, но **никогда** не ронять
    input-очередь,
  - метрики (fps, bitrate, encode time, RTT).
- **Concurrency:** отдельные goroutine на capture→encode, на запись в поток, на чтение
  ввода; общий `context.Context` для отмены.

#### `host/server`
- Listener (TCP control + UDP video), accept loop, лимит соединений, pairing/consent,
  жизненный цикл per-connection, переиспользование `host/session`.

### 4.3. Client

#### `client/decode`
- **Роль:** bitstream → кадр (YUV/RGB/GPU-текстура).
- **Интерфейс `Decoder`:** `Open(params)`, `Decode(packet) (*Frame, error)`, `Close()`.
- **Реализации:** `decode/openh264.go` (просто, CPU), `decode/ffmpeg_linux.go` (cgo,
  software или VAAPI hardware).
- **Почему интерфейс:** software на старте, hardware-декод позже, ради latency и CPU.

#### `client/render`
- **Роль:** показать кадр и собрать локальный ввод.
- **Интерфейс `Renderer`:** `Init(window)`, `Draw(*Frame)`, `Events() <-chan InputEvent`,
  `Stats()`, `Close()`.
- **Реализации:** `render/ebiten.go` (быстрый прототип), `render/opengl_glfw.go`
  (контроль над latency и шейдерами), позже Vulkan.
- **Почему интерфейс:** Ebiten скрывает многое и удобен для Phase 5; для тонкой оптимизации
  нужен прямой OpenGL.

#### `client/input`
- **Роль:** захват локального ввода и преобразование в RCUPC input-события.
- **Интерфейс `Capturer`:** `Start()`, `Events() <-chan InputEvent`, `Stop()`.
- **Реализации:** `input/x11_linux.go` (XInput2/XGrabPointer), позже Wayland через
  portals/libei. **X11 — приоритет**, Wayland-ограничения документируются.
- **Важно:** не путать `client/input` (кто читает локальный ввод) и `render` (кто рисует).

#### `client/session`
- **Роль:** зеркало host/session на клиенте.
- **Отвечает за:** handshake, demux, latest-frame slot для видео, отправку input,
  heartbeat/RTT, reconnect, статистику.

#### `client/app`
- Точка сборки: конфиг → transport → session → decode → render → input loop.
- Горячие клавиши (выход, переключение fullscreen, «release input»), tray/окно.

### 4.4. Границы пакетов — кратко

| Граница | Интерфейс | Кто меняется без последствий |
|---|---|---|
| capture ↔ session | `capture.Capturer`, `capture.Frame` | GDI ↔ DXGI ↔ WGC |
| encode ↔ session | `encode.Encoder` | MJPEG ↔ H.264 ↔ vendor |
| session ↔ transport | `transport.Stream` / `DatagramConn` | TCP ↔ UDP ↔ QUIC |
| decode ↔ session | `client/decode.Decoder` | openh264 ↔ ffmpeg SW/HW |
| render ↔ app | `client/render.Renderer` | Ebiten ↔ OpenGL ↔ Vulkan |
| input capture ↔ session | `client/input.Capturer` | X11 ↔ Wayland/libei |
| host input ↔ session | `host/input.Backend` | SendInput ↔ (future) virtual HID |

---

## 5. Потоки данных

### 5.1. Host → Client (видео)

1. **Capture goroutine** получает кадр из DXGI/GDI. Ставит его в **latest-frame slot**
   (`buffered chan cap=1` с non-blocking replace). Если encoder занят — старый кадр
   выбрасывается. Это ключ к низкой задержке.
2. **Encode goroutine** берёт кадр, при необходимости конвертирует (BGRA→NV12) на GPU,
   кодирует, выдаёт один или несколько `Packet` (фрагменты кадра).
3. **Mux/Writer goroutine** пишет пакеты в `DatagramConn` (или временно в TCP stream).
   Для видео последовательность: frame_id, fragment_index/count, keyframe flag, timestamp.
4. **Client reader** принимает пакеты, собирает фрагменты по frame_id, отдаёт полный кадр
   в **decode вход**.
5. **Decode** декодирует; результат кладётся в **latest-frame slot** перед render.
6. **Render loop** (обычно vsync/60–144 Гц) берёт последний кадр и рисует.

### 5.2. Client → Host (input)

1. **Input capturer** читает X11-события, нормализует (абс. координаты в 0..65535 или
   относительные дельты), шлёт `InputEvent` в канал.
2. **Client session** сериализует в `protocol` INPUT и пишет в **надёжный** stream.
3. **Host reader goroutine** принимает, demux'ит по типу, кладёт в **приоритетную очередь ввода**.
4. **Input applier** (отдельная goroutine, чтобы video не задерживал ввод) вызывает
   `SendInput`.

### 5.3. Concurrency model (goroutines/channels)

| Goroutine | Сторона | Задача | Канал |
|---|---|---|---|
| capture loop | host | получить кадр | `frames chan *Frame` (cap 1) |
| encoder loop | host | сжать | `packets chan Packet` (bounded) |
| net writer | host | писать в транспорт | исходящий канал / прямой write |
| net reader | host | читать control/input | обработка синхронно/per-msg |
| input applier | host | SendInput | приоритетная очередь |
| net reader | client | читать видео/control | сборка фрагментов |
| decoder loop | client | декодировать | `decoded chan *Frame` (cap 1) |
| render loop | client | рисовать (main thread для GUI!) | берёт latest |
| input loop | client | читать локальный ввод | `events chan InputEvent` |
| stats/heartbeat | обе | RTT, fps, bitrate | тикер |

Правила:
- **Никогда не блокировать capture.** Полный канал → drop.
- **Никогда не блокировать input за видео.** Разные очереди и, в идеале, разные сокеты.
- **Отмена через `context.Context`.** Все loop'ы завершаются по `ctx.Done()`.
- **GUI должен рисовать в своём потоке** (X11/GLFW/Ebiten требуют main thread). Render loop
  не должен быть обычной goroutine, если библиотека требует main.

### 5.4. Где нельзя копировать кадр

- **GPU→CPU readback** — самый дорогой переход (сотни микросекунд и bandwidth). DXGI даёт
  текстуру; encoder (NVENC/MF) умеет принимать D3D11-текстуру напрямую. Не делать
  `Map` только чтобы «посмотреть».
- **BGRA→NV12** — по возможности GPU compute/shader, а не CPU-цикл.
- **Fragmenting** — не копировать кадр на каждый фрагмент; писать заголовок + слайс в буфер.
- **Decode → render** — по возможности GPU-текстура (VAAPI/EGL), а не CPU→upload.
- **Буфер-пулы.** `sync.Pool` для пакетов/буферов, чтобы не давить на GC. Ссылка на буфер
  возвращается в пул после отправки/рендера.
- **Кольцевой буфер** для UDP-приёма, чтобы не аллоцировать на каждый пакет.

### 5.5. Потенциальные bottleneck'и

| Место | Причина | Как смягчить |
|---|---|---|
| GPU→CPU readback | bandwidth/латентность | encoder берёт текстуру; MJPEG только на старте |
| CPU-кодирование | нет hardware | NVENC/MF; иначе снизить разрешение/fps |
| CPU-декодирование на Linux | нет VAAPI | hardware decode; ниже разрешение |
| Копирование/GC | аллокация на кадр | пулы, кольцевые буферы |
| Channel contention | один большой канал | отдельные каналы video/input/control |
| Send buffer ОС | переполнение | дропать кадры, не копить; контролировать bitrate |
| Ширина LAN (Wi-Fi!) | bandwidth/jitter | H.264 вместо MJPEG; не полагаться на GbE по Wi-Fi |
| vsync/tearing | render привязан к монитору | отключать vsync для измерений, включать для плавности |
| Драйвер мыши/DPI | координатная неоднозначность | явные abs/rel режимы, DPI-нормализация |

### 5.6. Измерение задержки

Каждый кадр несёт `capture_ts` (монотонное время host). Плюс собственные seq/ts на этапах.
Host может считать encode+send time; client — network+decode+render. Для end-to-end нужна
грубая синхронизация часов (или использование RTT/2 как оценки). Метрики — обязательны,
иначе «оптимизация» превращается в угадывание.

---

## 6. Сравнение transport

| Вариант | Плюсы | Минусы | Задержка | Сложность | Для LAN realtime |
|---|---|---|---|---|---|
| **TCP** | просто, надёжно, упорядоченно, готовый TLS | head-of-line blocking; при потере лаг вверх | низкая при 0 потерь | низкая | **Да для control/input**, плохо для видео под нагрузкой |
| **UDP (свой слой)** | минимум задержки, контроль над всем | надо самому делать seq/ack/FEC/MTU/reassembly | минимальная | средняя/высокая | **Да для видео** (с FEC/NACK), но много работы |
| **QUIC (quic-go)** | TLS 1.3 встроен, multiplex streams, нет HOL между streams, RFC 9221 datagrams, congestion control | зависимость, сложнее отлаживать, overhead для LAN | низкая | средняя | **Да** как хороший компромисс; datagrams для видео |
| **WebRTC (Pion)** | RTP/SRTP, jitter buffer, NACK/FEC, ICE, congestion control «из коробки» | огромный стек, много «магии» скрыто, overkill для LAN, тяжёлые deps | низкая | высокая | Работает, но скрывает то, что хочется понять |
| **SRT** | создан для low-latency video, устойчив к потерям | Go-биндинги слабые/через cgo, меньше контроля | низкая | средняя/высокая | Да, но экосистема хуже для Go |

### 6.1. Рекомендация

**Поэтапно и гибридно:**

1. **Phase 1–5:** всё по **TCP**. Максимально просто, позволяет отработать handshake, протокол,
   capture, первый кадр, ввод. На LAN с малыми потерями даже видео по TCP работает.
2. **Phase 6:** отдельный **UDP data plane для видео**: свои sequence numbers, фрагментация
   по MTU, «latest-frame», опционально FEC и NACK. Control/input остаются на TCP.
3. **Позже (опционально):** **QUIC** как единая альтернатива — надёжный stream для
   control/input + datagrams для видео, с TLS 1.3 из коробки. Хороший путь к Phase 11
   (шифрование) без ручного Noise.

**Почему не WebRTC/Pion сейчас:** он решает задачи lossy Internet, которые в LAN неактуальны,
а взамен прячет механизмы, которые пользователь хочет понимать. Его можно рассмотреть позже
как готовый data plane, но не как основу обучения.

**Почему не только TCP:** видео по TCP при малейшей потере/переполнении буфера даёт
нелинейный рост задержки (retransmit + HOL). Именно это убивает «realtime».

**Почему не сразу QUIC:** это усложняет отладку первых фаз. Но `transport` интерфейс
проектируется так, чтобы QUIC добавить без переписывания.

---

## 7. Сравнение Windows capture

| Вариант | Плюсы | Минусы | Задержка | Сложность | Итог |
|---|---|---|---|---|---|
| **DXGI Desktop Duplication** (`IDXGIOutputDuplication`) | GPU-текстура, dirty/move rects, минимальная задержка, штатный путь Win8+ | API капризен: `AccessLost` при смене разрешения/полноэкранных приложениях, multi-GPU, только D3D11, COM | минимальная | высокая (COM, cgo) | **Основной путь** |
| **Windows.Graphics.Capture (WGC)** | современно (Win10 1803+), per-window capture, GPU-текстура, учитывает cursor | WinRT interop из Go тяжёл, версия ОС, меньше примеров для Go | низкая | высокая | Позже / опция |
| **GDI `BitBlt`** | просто, чисто Win32, любой Windows, легко из Go | CPU, нет GPU, медленно на больших разрешениях/high-DPI, без dirty rects | средняя/высокая | низкая | **Временный старт (Phase 3)** |
| **Windows Media Foundation capture** | экосистема Microsoft | для screen capture не основной путь | средняя | средняя | Не использовать |
| **Vendor SDK (NVFBC/AMF capture)** | очень быстро на NVIDIA | vendor lock, сложно, не универсально | минимальная | высокая | Только если упрёмся |

### 7.1. Рекомендация

- **Phase 3:** GDI `BitBlt` — чтобы получить живой кадр и отладить pipeline без COM/cgo.
- **Phase 6+:** **DXGI Desktop Duplication** как основная реализация. Написать устойчивую
  обработку `DXGI_ERROR_ACCESS_LOST`, смены разрешения, multi-monitor, DPI.
- **Компилятор:** DXGI/D3D11 из чистого Go через ручные COM-vtable крайне трудоёмок. Разумно
  держать **небольшой C/C++ shim** в `third_party/dxgi_capture`, который экспортирует простой
  C-API (`create`, `acquire_frame`, `release_frame`), а Go оборачивает его. Это осознанное
  исключение из «только Go» ради управляемости.
- Курсор: DXGI может отдавать отдельную маску курсора; на первом этапе можно рисовать курсор
  на клиенте локально (меньше задержка), позже — интегрировать.

---

## 8. План video pipeline

### 8.1. Стадии

```
[DXGI texture] → (optional GPU convert BGRA→NV12) → [Encoder]
     → [fragmentation по MTU] → [transport] → [reassembly]
     → [Decoder] → [GPU upload / texture] → [Renderer]
```

### 8.2. Решения по кодекам

| Кодек | Плюсы | Минусы | Bitrate 1080p60 | Роль |
|---|---|---|---|---|
| **Raw** | нет кодирования | ~2 ГБ/с BGRA — нереально даже LAN | — | только тест очень малого окна |
| **MJPEG** | просто, кадровая независимость (нет «keyframe stall»), низкая latency | большой bitrate, нет межкадрового сжатия | 50–400 Мбит/с | **Phase 5**, отладка |
| **H.264** | hardware везде, low latency, хорошее сжатие, зрелая экосистема | нужен encoder/decoder, важны keyframe/GOP | 5–40 Мбит/с | **Основной** |
| **H.265** | +30–50% сжатия | хуже совместимость, патенты, выше latency/сложность | 3–25 Мбит/с | Не сейчас |
| **AV1** | лучшее сжатие | realtime low-latency encoder'ы слабые/отсутствуют | — | Не сейчас |

**Вывод:** MJPEG для первого рабочего кадра, H.264 hardware — целевой. H.265/AV1 отложить.

### 8.3. Параметры, критичные для latency

- **Rate control:** CBR (или capped VBR), маленький VBV/буфер, чтобы не было burst-lag.
- **GOP:** короткий (ключевой кадр часто) для быстрого восстановления после потерь, но это
  повышает bitrate. На LAN допустим GOP ~1–2 секунды + `RequestKeyframe` по NACK.
- **Slices/tiles:** несколько slices позволяют декодировать/фрагментировать параллельно и
  ограничивают эффект потери.
- **B-frames: 0.** B-frames = реордеринг = задержка. Только P (low-latency).
- **Lookahead: 0.** Никакого будущего контекста.
- **Intra-refresh** как альтернатива keyframe (плавнее bitrate, медленнее восстановление).

### 8.4. Zero-copy цели

- DXGI texture → NVENC/MF напрямую (зарегистрировать D3D11-текстуру).
- NV12 conversion на GPU (video processor MFT / shader).
- На Linux: VAAPI/EGL decode → GL texture без CPU.
- На старте (GDI+MJPEG) копий будет много — это приемлемая цена за простоту.

---

## 9. План input pipeline

### 9.1. События

- Mouse: move (absolute/relative), button down/up (L/R/M/X1/X2), wheel (vertical/horizontal).
- Keyboard: key down/up (scancode + virtual key + modifiers), unicode text (для layout-независимого
  ввода), повтор (autorepeat делается на host при удержании).
- Спец: release-all (при потере окна), переключение захвата.

### 9.2. Client capture (Linux)

- **X11:** XInput2 (raw events) + `XGrabPointer`/`XGrabKeyboard` для захвата. Прячет локальный
  курсор, отправляет дельты/абсолют. X11/XTest — не для инъекции на клиенте, а для тестов.
- **Wayland:** приложения не могут читать чужой ввод. Варианты — libei/`org.freedesktop.portal.RemoteDesktop`
  (согласие пользователя через portal) или полноэкранное окно с относительным указателем.
  **Wayland — позже**, документируется как ограничение.
- **Режимы мыши:** absolute (по умолчанию, удобно для GUI) и relative (для игр/захвата).

### 9.3. Передача

- Input идёт по **надёжному** каналу. Каждое событие маленькое; задержка важнее экономии.
- Батчинг: несколько накопленных событий можно склеить в одно сообщение, если они пришли
  между тиками сети (снижает overhead), но не задерживать их искусственно.
- **Не дедуплицировать move'ы агрессивно**, если это заметно визуально; можно скидывать
  промежуточные move, оставляя последний (для absolute это ок).

### 9.4. Host apply

- `SendInput` — базовая реализация. Абсолютные координаты нормализуются в 0..65535 с учётом
  виртуального рабочего стола и DPI.
- Keyboard: `KEYEVENTF_SCANCODE` + `KEYEVENTF_EXTENDEDKEY` по возможности — так корректнее
  работают игры; virtual key как fallback.
- Порядок и тайминг: применять в отдельной высокоприоритетной goroutine.
- Ограничения: secure desktop (UAC), античит/антивирус могут помечать инъекцию.

### 9.5. Обратная связь для задержки

- Клиент локально рисует курсор по последней позиции (нулевая задержка ощущения). Host при
  этом двигает настоящий курсор.
- Для relative-режима — pointer lock на клиенте.

---

## 10. Риски и технические проблемы

### 10.1. Платформа / сборка

- **cgo на Windows из Linux.** Кросс-компиляция DXGI/MF shim'ов требует mingw-w64/zig cc или
  сборки на Windows. Рекомендация: собирать host **на Windows**, client — на Linux; CI
  отдельный по платформам.
- **COM из Go.** Даже с cgo shim'ом нужно аккуратно управлять COM-аппартментами и потоками.
- **Build tags.** Платформенные файлы легко рассинхронизировать; нужен CI на обеих ОС.

### 10.2. Capture / encode

- `DXGI_ERROR_ACCESS_LOST` при смене разрешения, UAC, входе в полноэкранный эксклюзивный режим.
  Нужен автоматический re-init.
- **Multi-monitor / DPI / HDR.** Отдельная головная боль; на ранних фазах — один монитор, SDR.
- **Hardware encoder фрагментация.** MF MFT есть не на всех GPU/драйверах; нужен выбор и fallback.
- **Патентные/лицензионные нюансы H.264/H.265** — учитывать при распространении (не блокер
  для личного проекта).
- Захват **protected content** (DRM) не сработает — это ожидаемо.

### 10.3. Сеть

- Wi-Fi даёт jitter и потери даже «в LAN»; UDP-план должен это переживать (FEC/NACK).
- MTU/IP-фрагментация: держать payload под ~1200 байт, иначе рост потерь.
- Симметрия: video от host→client большой, input client→host маленький, но при потере
  control-соединения всё разваливается — heartbeat обязателен.

### 10.4. Linux rendering / input

- **Wayland** ограничивает глобальный захват ввода и порой позиционирование окна.
- Смешение X11/Wayland сессий, fractional scaling, несколько мониторов у клиента.
- Vsync может добавлять кадр задержки; для измерений отключать.
- Выбор библиотеки рендера влияет на поток (main thread) и сложность.

### 10.5. Задержка и её измерение

- Без синхронизации часов end-to-end latency точно не измерить; использовать оценки (RTT,
  локальные времена этапов) и графики распределения, а не средние.
- GC-паузы при аллокациях на кадр — реальный источник «дрожания».

### 10.6. Безопасность / эксплуатация

- `SendInput` вне secure desktop; возможные ложные срабатывания защит.
- Открытый порт без pairing = риск управления чужим ПК. Даже в LAN — обязательный consent.

### 10.7. Организационные

- Соблазн «оптимизировать раньше времени» (H.265, Vulkan, собственный congestion control).
- Расползание монолита, если интерфейсы не соблюдать с самого начала.

---

## 11. Что НЕ нужно делать на ранних этапах

- ❌ Relay/cloud/STUN/TURN/NAT traversal — не нужно вообще.
- ❌ WebRTC/Pion как основа — откладывается.
- ❌ H.265/AV1 и «идеальный» кодек — позже и только при доказанной нужде.
- ❌ Собственный congestion control / BBR-like — не раньше, чем появится UDP data plane.
- ❌ Полноценная security-система — интерфейсы да, реализация на Phase 11.
- ❌ Audio — Phase 10.
- ❌ GUI «как в реальных продуктах» — Phase 12; на старте достаточно окна с кадром.
- ❌ Multi-monitor, HDR, multi-session, переподключение посреди потока — позже.
- ❌ Kernel-драйверы (виртуальный HID, свой capture driver) — вне целей.
- ❌ Преждевременная оптимизация zero-copy до того, как есть работающий pipeline и метрики.
- ❌ Wayland-поддержка ввода до того, как X11-путь работает.
- ❌ Своя реализация H.264 — всегда используем ffmpeg/openh264/vendor SDK.
- ❌ Абстракции «на будущее» без конкретного потребителя.
```