# RCUPC — Remote Control PC

Лёгкий remote-desktop для управления Windows PC с Linux-ноутбука по локальной сети.
Написан на Go. Основной приоритет — **LAN и минимальная задержка**, а не Internet/cloud.

```
Windows PC  ──── LAN ────▶  Linux laptop
 (Host)                      (Client)
```

## Статус

**Phase 1–9 реализованы:** TCP-соединение, handshake
(`MIRROR/0.1 HELLO` → `MIRROR/0.1 OK`), message-фаза (`PING`, `INFO`), выделенная
граница транспорта (`pkg/transport`), graceful shutdown через `context.Context`,
структурированное логирование (`pkg/logging`), слой конфигурации (`pkg/config`),
захват кадра рабочего стола (`pkg/capture`), конвейер кадров (`pkg/framepipe`) и
MJPEG-энкодер (`pkg/encoder`). Только стандартная библиотека, без зависимостей.

Следующая по плану — Phase 10. См. [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Phase 1 — быстрый старт

### Windows (Host)

```powershell
go build -o rcupc-host.exe ./cmd/rcupc-host
.\rcupc-host.exe            # слушает :9000
.\rcupc-host.exe -addr :9000
```

Узнать локальный IP:

```powershell
ipconfig                  # смотреть "IPv4-адрес" активного адаптера, напр. 192.168.1.42
Get-NetIPAddress -AddressFamily IPv4   # альтернатива
```

### Linux (Client)

```bash
go build -o rcupc-client ./cmd/rcupc-client
./rcupc-client <WINDOWS_IP>:9000
```

Прервать — `Ctrl+C`; сессия закрывается корректно.

### Кросс-сборка host под Windows с Linux

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/rcupc-host.exe ./cmd/rcupc-host
```

### Пример успешного соединения

Host (Windows):

```
time=... level=INFO msg=listening addr=[::]:9000
time=... level=INFO msg="client connected" remote=192.168.1.77:51422
time=... level=INFO msg="handshake complete" remote=192.168.1.77:51422 protocol=MIRROR/0.1
time=... level=INFO msg="session closed" remote=192.168.1.77:51422
```

Client (Linux):

```
$ ./rcupc-client 192.168.1.42:9000
time=... level=INFO msg=connecting addr=192.168.1.42:9000
time=... level=INFO msg="connection established" addr=192.168.1.42:9000
^C
time=... level=INFO msg="connection closed" addr=192.168.1.42:9000
```

Неверный handshake — host сообщает причину и закрывает соединение:

```
$ printf 'WRONG/1.0 HELLO\n' | nc 192.168.1.42 9000
MIRROR/0.1 ERROR protocol: unsupported version "WRONG/1.0" (want "MIRROR/0.1")
```

## Phase 2 — message layer

После успешного handshake соединение переходит в message-фазу: команды и ответы —
простые newline-delimited строки ASCII/UTF-8 (без префикса версии).

| Client → Host | Host → Client |
|---|---|
| `PING\n` | `PONG\n` |
| `INFO\n` | `RCUPC/0.1 HOST\n` |
| любая другая строка | `ERROR unknown command\n` |
| строка длиннее 256 байт | `ERROR line too long\n`, затем соединение закрывается |

Соединение остаётся открытым между командами; неизвестная команда не рвёт сессию.
Ограничение длины строки защищает от неограниченного буферизования, а закрытие одного
соединения завершает только эту сессию, не весь host.

Пример: клиент после подключения сам отправляет `INFO` и `PING`:

```
$ ./rcupc-client 192.168.1.42:9000
time=... level=INFO msg=connecting addr=192.168.1.42:9000
time=... level=INFO msg="connection established" addr=192.168.1.42:9000
time=... level=INFO msg=command send=INFO recv="RCUPC/0.1 HOST"
time=... level=INFO msg=command send=PING recv=PONG
^C
time=... level=INFO msg="connection closed" addr=192.168.1.42:9000
```

Ту же message-фазу можно прогнать вручную по TCP:

```
$ printf 'MIRROR/0.1 HELLO\nINFO\nPING\nBOGUS\n' | nc 192.168.1.42 9000
MIRROR/0.1 OK
RCUPC/0.1 HOST
PONG
ERROR unknown command
```

## Архитектура — граница транспорта (Phase 3)

Сетевые детали собраны в одном пакете, чтобы протокол и сессии не зависели от TCP:

| Пакет | Отвечает за |
|---|---|
| `pkg/protocol` | только формат сообщений (handshake и message-фаза); не знает про сеть |
| `pkg/transport` | TCP-транспорт: `Listen`, `Accept`, `Dial` и `Conn` (read/write/deadline/close/RemoteAddr) |
| `host/session`, `client/session` | handshake и обработка команд поверх `transport.Conn` |
| `host/server` | accept через `transport` и запуск отдельной сессии на каждое соединение |
| `client/app` | wiring приложения |

`pkg/protocol` не импортирует `transport`. Сессии не используют `net` напрямую:
`transport.Conn` — это интерфейс (read/write/close/deadline/remote), поэтому сессии
тестируются через `net.Pipe`, а в продакшене работает TCP.

Пример подключения и message-фазы не изменился — см. разделы Phase 1 и Phase 2 выше.

## Phase 4 — graceful shutdown (context)

Время жизни соединения управляется `context.Context`; отдельного lifecycle-менеджера нет,
используются стандартные `context` и `transport.Conn`.

**Host** (`server.ListenAndServe(ctx)`), при отмене контекста:

1. перестаёт принимать новые соединения (listener закрывается);
2. завершает активные сессии: watcher закрывает соединение и разблокирует `Read`;
3. дожидается завершения goroutine сессий (`sync.WaitGroup`);
4. возвращает `nil` — процесс изнутри пакета `server` не завершается.

**Client** (`session.Wait(ctx)`), при отмене контекста:

1. закрывает соединение (разблокирует `Read`);
2. завершает цикл чтения;
3. возвращает `ctx.Err()` (`context.Canceled`); приложение трактует это как чистое завершение.

Закрытие соединения — единственный способ разбудить заблокированный `Read`, без polling-циклов.
Отмена означает shutdown, а не reconnect.

Пример: `Ctrl+C` на host во время активной сессии (`INFO`/`PING` уже обслужены):

```
host   exit status: 0   ... command handled ... session closed
client exit status: 0   ... connection established ... connection closed
новое подключение: connection refused (listener закрыт)
```

Wire-протокол не менялся: те же handshake и команды.

## Phase 5 — structured logging (pkg/logging)

Единая маленькая обёртка над стандартным `log/slog`, без глобального состояния: логгер
создаётся явно (`logging.New(os.Stderr, logging.LevelInfo)`) и передаётся в компоненты.
Интерфейс `logging.Logger` (`Debug`/`Info`/`Warn`/`Error`, сообщение + поля) позволяет
подставлять в тестах свой recorder. Реализация concurrency-safe (несколько сессий пишут
одновременно).

Что и на каком уровне пишется:

| Сторона | Info | Warn | Error |
|---|---|---|---|
| Host | listening, client connected, handshake complete, command handled, session closed, server shutdown | invalid handshake, invalid command, oversized message | handshake/read/write failures |
| Client | connecting, handshake complete, connected, command sent, response received, connection closed, shutdown | — | connect/handshake failures |

Ошибка логируется один раз на своём слое (сессия); сервер фиксирует только жизненный цикл.
Токенов, паролей, дампов payload'ов и построения по байтам соединения в логах нет.

Пример:

```
host:   listening → client connected → handshake complete
        → command handled (INFO, PING) → session closed → server shutdown
client: connecting → handshake complete → connected
        → command sent / response received (INFO, PING) → connection closed
```

## Phase 6 — configuration (pkg/config)

Runtime-настройки вынесены в `pkg/config`: обычные структуры `HostConfig` /
`ClientConfig`, значения по умолчанию в `config.DefaultHost()` / `config.DefaultClient()`,
определения флагов там же (`config.BindHost` / `config.BindClient`), а `Validate()` вызывается
до старта. После старта конфигурация только читается (immutable), глобальных переменных нет.
Парсинг флагов живёт в `cmd`, а не в session/app.

### Флаги и значения по умолчанию

Host (`rcupc-host`):

| Флаг | По умолчанию | Описание |
|---|---|---|
| `-addr` | `:9000` | TCP-адрес прослушивания |
| `-handshake-timeout` | `5s` | таймаут handshake |
| `-max-line-len` | `256` | максимальная длина строки сообщения, байт |

Client (`rcupc-client [flags] <host>:<port>`) — адрес задаётся позиционно:

| Флаг | По умолчанию | Описание |
|---|---|---|
| `-handshake-timeout` | `5s` | таймаут handshake |
| `-max-line-len` | `256` | максимальная длина строки сообщения, байт |

### Примеры

```bash
# Host по умолчанию
./rcupc-host

# Host на другом интерфейсе/порту с другими лимитами
./rcupc-host -addr 192.168.1.42:9100 -handshake-timeout 3s -max-line-len 1024

# Client (флаги указываются до адреса)
./rcupc-client 192.168.1.42:9000
./rcupc-client -handshake-timeout 3s -max-line-len 1024 192.168.1.42:9100
```

Некорректная конфигурация (`-handshake-timeout 0s`, `-max-line-len 0`, адрес без порта)
даёт понятную ошибку и выход с кодом `2`; сервер при этом не запускается.

## Phase 7 — Windows desktop capture foundation (pkg/capture)

Появился изолированный пакет `pkg/capture` — первый шаг к захвату экрана. Он получает
**один сырой кадр** рабочего стола и ничего не знает про TCP, сессии, протокол, клиент,
сервер или реализацию логирования.

API:

```
Source interface { Start() error; Capture() (*Frame, error); Close() error }

Frame { Width, Height, Stride int; Format PixelFormat; Pixels []byte }
```

- **Windows backend:** GDI (`BitBlt` + `GetDIBits`) через стандартный `syscall`
  (`user32.dll` / `gdi32.dll`) — без сторонних зависимостей. Захватывается только основной
  (primary) монитор. Ресурсы (screen DC, memory DC, bitmap) создаются в `Start` и
  освобождаются в `Close`; `Close` идемпотентен, `Capture` после `Close` возвращает ошибку.
- **Не-Windows:** `capture.New()` возвращает `capture.ErrUnsupported`
  («desktop capture is unsupported on this platform»). Linux не является capture-host и
  **не** отдаёт фейковые кадры.
- **Формат:** BGRA, top-down. GDI отдаёт строки снизу вверх, поэтому строки переворачиваются.
- Кадр пока **не кодируется и не передаётся по сети** — это будущие фазы.
- Multi-monitor, HDR, dirty-rect, frame-rate control, DXGI и аппаратное кодирование — вне Phase 7.

Проверка на Windows (нужна интерактивная desktop-сессия):

```powershell
go test -run TestCaptureOneFrame -v ./pkg/capture
```

Тест инициализирует GDI, снимает один кадр, печатает размеры/формат, проверяет непустой
буфер и корректно освобождает ресурсы.

## Phase 8 — frame pipeline (pkg/framepipe)

Появился небольшой конвейер, который многократно снимает кадры из `capture.Source` и
передаёт их потребителю. Он не знает про TCP, протокол, сессии, клиент, сервер,
кодирование или GUI и зависит только от `pkg/capture`.

API:

```
Consumer interface { Consume(*capture.Frame) error }

New(src capture.Source, consumer Consumer, interval time.Duration) (*Pipeline, error)
(p *Pipeline) Run(ctx context.Context) error
```

- `Run` вызывает `Start` у источника, в цикле снимает кадры, отдаёт их в `Consume` и
  завершается по отмене `context`; источник закрывается **ровно один раз**.
- Интервал между кадрами задаёт вызывающий; нулевой/отрицательный отвергается в `New`.
  Отдельной FPS-настройки пока нет.
- Ошибка `Capture`, ошибка потребителя или невалидный/пустой кадр останавливают конвейер
  с возвратом ошибки — бесконечного плотного цикла при ошибке нет.
- Отмена: новые кадры не снимаются и не доставляются; `Run` возвращает `ctx.Err()`
  (ошибка `Close`, если есть, присоединяется через `errors.Join`).
- Владение: `Capture` создаёт новый кадр, конвейер передаёт его потребителю и не
  переиспользует буфер; пулы буферов не вводятся.
- Конвейер пока **не подключён** к host/session, кадры не кодируются и не передаются по сети.

## Phase 9 — first frame encoder (pkg/encoder)

Появился MJPEG-энкодер: каждый сырой `capture.Frame` независимо кодируется в JPEG.
Зависит только от `pkg/capture` и стандартного `image/jpeg`; не знает про TCP, протокол,
сессии, `framepipe`, логирование или конфигурацию.

API:

```
Encoder interface { Encode(*capture.Frame) (*EncodedFrame, error) }
NewJPEG(quality int) (*JPEGEncoder, error)

EncodedFrame { Format Format; Width, Height int; Data []byte }
```

- BGRA → RGBA с учётом `Stride` (может быть больше `width*4`) и перестановкой каналов;
  исходный кадр не мутируется.
- JPEG quality задаётся в конструкторе, диапазон `1..100`; неверное значение — ошибка,
  а не молчаливый clamp. `encoder.DefaultQuality = 80`.
- Вход проверяется через `capture.Frame.Validate()`: nil, битые размеры/stride/буфер и
  неподдерживаемый формат отвергаются.
- `EncodedFrame.Data` принадлежит вызывающему; буферы не переиспользуются и не пулятся.
- Кадры пока **локальные**: не передаются по сети и ни к чему не подключены.
  H.264/H.265/аппаратное кодирование — будущие фазы.

## Цели

- Real-time просмотр рабочего стола Windows.
- Минимальная задержка видео.
- Инъекция mouse и keyboard событий.
- В перспективе — audio.
- Управление, близкое к «сижу за этим ПК».
- Понятная модульная архитектура, которую можно писать и понимать постепенно.

## Не-цели (сейчас)

- Internet / relay / cloud / STUN / TURN.
- Web-приложение.
- Electron.
- Сторонний сервер.
- Монолит.
- Полная security-система на ранних этапах.

## Карта документации

| Документ | Содержание |
|---|---|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Компоненты, схема, структура директорий, интерфейсы, потоки данных, сравнение transport и capture, video/input pipeline, риски |
| [`docs/PROTOCOL.md`](docs/PROTOCOL.md) | Собственный application protocol RCUPC: сообщения, framing, handshake, reconnect, heartbeat |
| [`docs/SECURITY.md`](docs/SECURITY.md) | Модель угроз, pairing, authentication, encryption, authorization |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | Phase 0 … Phase 12: цель, компоненты, результат, чему учит |

## Рекомендуемое чтение

1. `ARCHITECTURE.md` — понять границы компонентов и почему они такие.
2. `PROTOCOL.md` — понять, что именно летит по сети.
3. `SECURITY.md` — понять, какие «крючки» оставить уже сейчас.
4. `ROADMAP.md` — понять, в каком порядке всё это писать.

## Быстрые ключевые решения (обоснования внутри документов)

- **Capture:** GDI как временный старт → DXGI Desktop Duplication как основной путь.
- **Encode:** MJPEG для первого кадра → H.264 (hardware) для realtime.
- **Transport:** TCP для control/input → отдельный UDP data plane для видео → опционально QUIC.
- **Input host:** `SendInput` (user32), без kernel-драйверов.
- **Render Linux:** OpenGL/GLFW или Ebiten на старте, за интерфейсом `Renderer`.
- **Язык:** Go; платформенные куски — за build tags; узкие cgo/C++ shim'ы там, где иначе нельзя.
